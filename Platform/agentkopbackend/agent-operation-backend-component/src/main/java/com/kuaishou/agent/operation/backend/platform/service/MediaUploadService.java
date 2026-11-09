package com.kuaishou.agent.operation.backend.platform.service;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.net.URL;
import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Objects;
import java.util.UUID;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnBean;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;
import org.springframework.web.multipart.MultipartFile;

import com.amazonaws.services.s3.model.ObjectMetadata;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.MediaAttachmentMapper;
import com.kuaishou.agent.operation.backend.platform.dto.MediaUploadView;
import com.kuaishou.agent.operation.backend.platform.entity.MediaAttachment;
import com.kuaishou.blobstore.s3.BS3Client;
import com.kuaishou.cdn.api.dispatch.ApiCdnDispatch;
import com.kuaishou.cdn.common.model.CdnBizType;
import com.kuaishou.cdn.common.model.CdnDispatchContext;
import com.kuaishou.cdn.common.model.CdnDispatchRequest;
import com.kuaishou.cdn.common.model.CdnDispatchResponse;
import com.kuaishou.freetraffic.constant.FreeTrafficProduct;

/**
 * 图片 / 视频 / 音频 / 文档上传：Multipart → BlobStore → CDN，并落库绑定上传人。
 */
@Service
@ConditionalOnBean(BS3Client.class)
public class MediaUploadService {

    private static final Logger LOGGER = LoggerFactory.getLogger(MediaUploadService.class);

    private static final String[] IMAGE_TYPES = {
            "image/png", "image/jpeg", "image/jpg", "image/webp", "image/gif"
    };
    private static final String[] VIDEO_TYPES = {
            "video/mp4", "video/quicktime", "video/webm", "video/x-msvideo", "video/mpeg"
    };
    private static final String[] AUDIO_TYPES = {
            "audio/mpeg", "audio/mp3", "audio/mp4", "audio/m4a", "audio/x-m4a",
            "audio/wav", "audio/x-wav", "audio/wave", "audio/aac", "audio/ogg",
            "audio/webm", "audio/flac", "audio/x-flac"
    };
    private static final String[] DOC_TYPES = {
            "application/pdf",
            "application/msword",
            "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
            "application/vnd.ms-excel",
            "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
            "application/vnd.ms-powerpoint",
            "application/vnd.openxmlformats-officedocument.presentationml.presentation",
            "application/rtf",
            "text/rtf",
            "text/plain",
            "text/markdown",
            "text/csv"
    };
    // 压缩包上传暂不开放：agent-k 上游不能把 archive 作为用户附件消费，也不支持 read_file 解包读取。
    // private static final String[] ARCHIVE_TYPES = {
    //         "application/zip",
    //         "application/x-zip-compressed",
    //         "application/x-tar",
    //         "application/gzip",
    //         "application/x-gzip",
    //         "application/x-bzip2",
    //         "application/x-xz",
    //         "application/x-7z-compressed",
    //         "application/vnd.rar",
    //         "application/x-rar-compressed",
    //         "application/zstd",
    //         "application/x-zstd"
    // };

    private final BS3Client bs3Client;
    private final MediaAttachmentMapper mediaAttachmentMapper;

    @Value("${agent.operation.backend.media.bucket:mmu-playground}")
    private String bucketName;

    @Value("${agent.operation.backend.media.key-prefix:klingAgentK/}")
    private String keyPrefix;

    @Value("${agent.operation.backend.media.cdn-project-name:}")
    private String cdnProjectName;

    @Value("${agent.operation.backend.media.max-image-bytes:52428800}")
    private long maxImageBytes;

    @Value("${agent.operation.backend.media.max-video-bytes:209715200}")
    private long maxVideoBytes;

    @Value("${agent.operation.backend.media.max-audio-bytes:104857600}")
    private long maxAudioBytes;

    @Value("${agent.operation.backend.media.max-doc-bytes:52428800}")
    private long maxDocBytes;

    // @Value("${agent.operation.backend.media.max-archive-bytes:209715200}")
    // private long maxArchiveBytes;

    @Value("${agent.operation.backend.media.max-batch-files:10}")
    private int maxBatchFiles;

    public MediaUploadService(BS3Client bs3Client, MediaAttachmentMapper mediaAttachmentMapper) {
        this.bs3Client = bs3Client;
        this.mediaAttachmentMapper = mediaAttachmentMapper;
    }

    /**
     * 上传媒体并绑定上传人；成功 / 失败均落库。
     *
     * @param file      multipart 文件
     * @param mediaType 可选：{@code image} / {@code video} / {@code audio} / {@code doc}；空则按 Content-Type / 扩展名推断
     * @param userId    上传人 userId（可空，空串入库）
     */
    public MediaUploadView upload(MultipartFile file, String mediaType, String userId) {
        String owner = StringUtils.hasText(userId) ? userId.trim() : "";
        String resolvedType = "";
        String blobKey = null;

        try {
            if (file == null || file.isEmpty()) {
                throw new IllegalArgumentException("上传文件不能为空");
            }
            resolvedType = resolveMediaType(file, mediaType);
            validate(file, resolvedType);

            byte[] bytes = file.getBytes();
            String extension = extractExtension(file.getOriginalFilename(), file.getContentType(), resolvedType);
            blobKey = normalizePrefix(keyPrefix)
                    + UUID.randomUUID().toString().replace("-", "")
                    + "." + extension;
            String contentType = StringUtils.hasText(file.getContentType())
                    ? file.getContentType()
                    : defaultContentType(resolvedType, extension);

            putToBlobStore(blobKey, bytes, contentType);

            URL rawUrl = bs3Client.getUrl(bucketName, blobKey);
            String blobUrl = rawUrl != null ? rawUrl.toString() : null;
            String cdnUri = bs3Client.generateCdnUri(bucketName, blobKey);
            CdnBizType bizType = resolveCdnBizType(resolvedType);
            String cdnUrl = dispatchCdnUrl(cdnUri, bizType);
            if (!StringUtils.hasText(cdnUrl)) {
                cdnUrl = cdnUri;
            }

            Long attachmentId = persistAttachment(owner, resolvedType, MediaAttachment.STATUS_SUCCESS,
                    "", blobKey, blobUrl, cdnUrl);

            LOGGER.info("媒体上传成功 id={} userId={} mediaType={} blobKey={} size={}",
                    attachmentId, owner, resolvedType, blobKey, bytes.length);

            return MediaUploadView.builder()
                    .id(attachmentId)
                    .userId(owner)
                    .status(MediaAttachment.STATUS_SUCCESS)
                    .errorMessage("")
                    .fileId(blobKey)
                    .blobKey(blobKey)
                    .mediaType(resolvedType)
                    .blobUrl(blobUrl)
                    .fileUrl(blobUrl)
                    .cdnUrl(cdnUrl)
                    .contentType(contentType)
                    .size(bytes.length)
                    .originalFilename(file.getOriginalFilename())
                    .build();
        } catch (IllegalArgumentException e) {
            persistAttachment(owner, resolvedType, MediaAttachment.STATUS_FAILED,
                    e.getMessage(), blobKey, null, null);
            throw e;
        } catch (IOException e) {
            LOGGER.error("读取上传文件失败", e);
            String msg = "读取上传文件失败：" + e.getMessage();
            persistAttachment(owner, resolvedType, MediaAttachment.STATUS_FAILED,
                    msg, blobKey, null, null);
            throw new IllegalArgumentException(msg);
        } catch (Exception e) {
            LOGGER.error("上传到 BlobStore / CDN 签发失败", e);
            String msg = "媒体上传失败：" + e.getMessage();
            persistAttachment(owner, resolvedType, MediaAttachment.STATUS_FAILED,
                    msg, blobKey, null, null);
            throw new IllegalStateException(msg, e);
        }
    }

    /**
     * 批量上传入口。先完整校验数量与每个文件的类型/大小，再开始上传，避免明显参数错误造成部分成功。
     */
    public List<MediaUploadView> uploadBatch(MultipartFile[] files, String mediaType, String userId) {
        if (files == null || files.length == 0) {
            throw new IllegalArgumentException("上传文件不能为空");
        }
        int limit = normalizedMaxBatchFiles();
        if (files.length > limit) {
            throw new IllegalArgumentException("批量上传文件数量不能超过 " + limit + " 个");
        }
        for (MultipartFile file : files) {
            if (file == null || file.isEmpty()) {
                throw new IllegalArgumentException("上传文件不能为空");
            }
            validate(file, resolveMediaType(file, mediaType));
        }
        List<MediaUploadView> results = new ArrayList<>(files.length);
        for (MultipartFile file : files) {
            results.add(upload(file, mediaType, userId));
        }
        return results;
    }

    private Long persistAttachment(String userId, String mediaType, String status, String errorMessage,
            String blobKey, String blobUrl, String cdnUrl) {
        try {
            MediaAttachment row = new MediaAttachment();
            row.setUserId(userId == null ? "" : userId);
            row.setMediaType(mediaType == null ? "" : mediaType);
            row.setStatus(StringUtils.hasText(status) ? status : MediaAttachment.STATUS_FAILED);
            row.setErrorMessage(truncate(errorMessage, 512));
            row.setBlobKey(StringUtils.hasText(blobKey) ? blobKey : null);
            row.setBlobUrl(blobUrl);
            row.setCdnUrl(cdnUrl);
            row.setCreatedAt(LocalDateTime.now());
            mediaAttachmentMapper.insert(row);
            return row.getId();
        } catch (Exception e) {
            LOGGER.error("媒体附件落库失败 userId={} status={} blobKey={}", userId, status, blobKey, e);
            return null;
        }
    }

    private static String truncate(String raw, int max) {
        if (!StringUtils.hasText(raw)) {
            return "";
        }
        String text = raw.trim();
        return text.length() <= max ? text : text.substring(0, max);
    }

    private void validate(MultipartFile file, String mediaType) {
        long max = maxBytes(mediaType);
        if (file.getSize() > max) {
            throw new IllegalArgumentException(mediaType + " 大小不能超过 " + (max / (1024 * 1024)) + "MB");
        }
        String ext = filenameExtension(file.getOriginalFilename());
        // 浏览器常把 docx/pdf 等标成 octet-stream；有合法扩展名时以扩展名为准
        if (StringUtils.hasText(ext) && extensionMatchesMediaType(ext, mediaType)) {
            return;
        }
        String contentType = file.getContentType();
        if (!StringUtils.hasText(contentType) || isGenericBinaryContentType(contentType)) {
            if (StringUtils.hasText(ext)) {
                throw new IllegalArgumentException(mediaTypeError(mediaType));
            }
            return;
        }
        String ct = contentType.toLowerCase(Locale.ROOT);
        if ("image".equals(mediaType) && !isAllowed(ct, IMAGE_TYPES) && !ct.startsWith("image/")) {
            throw new IllegalArgumentException(mediaTypeError(mediaType));
        }
        if ("video".equals(mediaType) && !isAllowed(ct, VIDEO_TYPES) && !ct.startsWith("video/")) {
            throw new IllegalArgumentException(mediaTypeError(mediaType));
        }
        if ("audio".equals(mediaType) && !isAllowed(ct, AUDIO_TYPES) && !ct.startsWith("audio/")) {
            throw new IllegalArgumentException(mediaTypeError(mediaType));
        }
        if ("doc".equals(mediaType) && !isAllowed(ct, DOC_TYPES)
                && !ct.startsWith("text/")
                && !ct.contains("document") && !ct.contains("sheet") && !ct.contains("presentation")) {
            throw new IllegalArgumentException(mediaTypeError(mediaType));
        }
        // archive 暂不开放，避免上传成功但无法被 agent-k 作为附件或 read_file 内容消费。
        // if ("archive".equals(mediaType) && !isAllowed(ct, ARCHIVE_TYPES)
        //         && !ct.contains("zip") && !ct.contains("tar") && !ct.contains("gzip")
        //         && !ct.contains("bzip2") && !ct.contains("xz") && !ct.contains("rar")
        //         && !ct.contains("7z") && !ct.contains("zstd") && !ct.contains("compress")) {
        //     throw new IllegalArgumentException(mediaTypeError(mediaType));
        // }
    }

    private static String mediaTypeError(String mediaType) {
        if ("image".equals(mediaType)) {
            return "仅支持 png / jpg / jpeg / webp / gif 图片";
        }
        if ("video".equals(mediaType)) {
            return "仅支持 mp4 / mov / webm / avi / mpeg 视频";
        }
        if ("audio".equals(mediaType)) {
            return "仅支持 mp3 / m4a / wav / aac / ogg / flac 音频";
        }
        // if ("archive".equals(mediaType)) {
        //     return "仅支持 zip / tar / gz / tgz / bz2 / xz / 7z / rar / zst 压缩包";
        // }
        return "仅支持 pdf / doc / docx / xls / xlsx / ppt / pptx / txt / md / csv / rtf 文档";
    }

    private static boolean isGenericBinaryContentType(String contentType) {
        String ct = contentType.toLowerCase(Locale.ROOT).trim();
        return "application/octet-stream".equals(ct) || "binary/octet-stream".equals(ct);
    }

    private static boolean extensionMatchesMediaType(String ext, String mediaType) {
        if ("image".equals(mediaType)) {
            return isImageExt(ext);
        }
        if ("video".equals(mediaType)) {
            return isVideoExt(ext);
        }
        if ("audio".equals(mediaType)) {
            return isAudioExt(ext);
        }
        if ("doc".equals(mediaType)) {
            return isDocExt(ext);
        }
        // if ("archive".equals(mediaType)) {
        //     return isArchiveExt(ext);
        // }
        return false;
    }

    private static String filenameExtension(String filename) {
        if (!StringUtils.hasText(filename) || !filename.contains(".")) {
            return null;
        }
        String ext = filename.substring(filename.lastIndexOf('.') + 1).toLowerCase(Locale.ROOT);
        return StringUtils.hasText(ext) && ext.length() <= 8 ? ext : null;
    }

    private long maxBytes(String mediaType) {
        if ("video".equals(mediaType)) {
            return maxVideoBytes;
        }
        if ("audio".equals(mediaType)) {
            return maxAudioBytes;
        }
        if ("doc".equals(mediaType)) {
            return maxDocBytes;
        }
        // if ("archive".equals(mediaType)) {
        //     return maxArchiveBytes;
        // }
        return maxImageBytes;
    }

    private int normalizedMaxBatchFiles() {
        return maxBatchFiles <= 0 ? 1 : maxBatchFiles;
    }

    private String resolveMediaType(MultipartFile file, String requested) {
        if (StringUtils.hasText(requested)) {
            String t = requested.trim().toLowerCase(Locale.ROOT);
            if (!"image".equals(t) && !"video".equals(t) && !"audio".equals(t) && !"doc".equals(t)) {
                throw new IllegalArgumentException("mediaType 仅支持 image / video / audio / doc");
            }
            return t;
        }
        String contentType = file.getContentType();
        if (StringUtils.hasText(contentType)) {
            String ct = contentType.toLowerCase(Locale.ROOT);
            if (ct.startsWith("image/") || isAllowed(ct, IMAGE_TYPES)) {
                return "image";
            }
            if (ct.startsWith("video/") || isAllowed(ct, VIDEO_TYPES)) {
                return "video";
            }
            if (ct.startsWith("audio/") || isAllowed(ct, AUDIO_TYPES)) {
                return "audio";
            }
            if (isAllowed(ct, DOC_TYPES) || ct.startsWith("text/")
                    || ct.contains("document") || ct.contains("sheet") || ct.contains("presentation")) {
                return "doc";
            }
            // archive 暂不开放，避免上传成功但无法被 agent-k 作为附件或 read_file 内容消费。
            // if (isAllowed(ct, ARCHIVE_TYPES) || ct.contains("zip") || ct.contains("tar")
            //         || ct.contains("gzip") || ct.contains("bzip2") || ct.contains("xz")
            //         || ct.contains("rar") || ct.contains("7z") || ct.contains("zstd")
            //         || ct.contains("compress")) {
            //     return "archive";
            // }
        }
        String ext = filenameExtension(file.getOriginalFilename());
        if (StringUtils.hasText(ext)) {
            if (isImageExt(ext)) {
                return "image";
            }
            if (isVideoExt(ext)) {
                return "video";
            }
            if (isAudioExt(ext)) {
                return "audio";
            }
            if (isDocExt(ext)) {
                return "doc";
            }
            // if (isArchiveExt(ext)) {
            //     return "archive";
            // }
        }
        throw new IllegalArgumentException("无法识别媒体类型，请传 mediaType=image|video|audio|doc");
    }

    /** 音视频统一走 VIDEO 调度域；图片/文档走 IMAGE。 */
    private static CdnBizType resolveCdnBizType(String mediaType) {
        if ("video".equals(mediaType) || "audio".equals(mediaType)) {
            return CdnBizType.VIDEO;
        }
        return CdnBizType.IMAGE;
    }

    private void putToBlobStore(String key, byte[] data, String contentType) {
        ObjectMetadata metadata = new ObjectMetadata();
        metadata.setContentLength(data.length);
        metadata.setContentType(contentType);
        bs3Client.putObject(bucketName, key, new ByteArrayInputStream(data), metadata);
        ObjectMetadata verify = bs3Client.getObjectMetadata(bucketName, key);
        if (verify == null) {
            throw new IllegalStateException("上传后验证失败：无法读取对象元数据");
        }
    }

    private String dispatchCdnUrl(String urlPath, CdnBizType bizType) {
        if (!StringUtils.hasText(cdnProjectName)) {
            LOGGER.warn("CDN 项目名未配置，返回 generateCdnUri 原始路径: {}", urlPath);
            return urlPath;
        }
        try {
            CdnDispatchContext ctx = CdnDispatchContext.newBuilder()
                    .setFreeTrafficProduct(FreeTrafficProduct.UNKNOWN)
                    .setIsHttps(true)
                    .build();
            CdnDispatchRequest request = CdnDispatchRequest.newBuilder()
                    .setProjectName(cdnProjectName)
                    .setUrlPath(urlPath)
                    .setDispatchContext(ctx)
                    .build();
            List<CdnDispatchResponse> responses = ApiCdnDispatch.unionDispatch(request, bizType);
            if (responses == null || responses.isEmpty()) {
                LOGGER.warn("CDN 调度结果为空，使用原始 URI: {}", urlPath);
                return urlPath;
            }
            String dispatched = responses.get(0).getUrl();
            LOGGER.info("CDN 调度成功 path={} cdnUrl={}", urlPath, dispatched);
            return dispatched;
        } catch (Exception e) {
            LOGGER.error("CDN 调度失败，使用原始 URI: {}", urlPath, e);
            return urlPath;
        }
    }

    private static String normalizePrefix(String prefix) {
        if (!StringUtils.hasText(prefix)) {
            return "";
        }
        return prefix.endsWith("/") ? prefix : prefix + "/";
    }

    private static boolean isAllowed(String contentType, String[] allowed) {
        for (String a : allowed) {
            if (Objects.equals(a, contentType)) {
                return true;
            }
        }
        return false;
    }

    private static String extractExtension(String filename, String contentType, String mediaType) {
        if (StringUtils.hasText(filename) && filename.contains(".")) {
            String ext = filename.substring(filename.lastIndexOf('.') + 1).toLowerCase(Locale.ROOT);
            if (StringUtils.hasText(ext) && ext.length() <= 8) {
                return ext;
            }
        }
        String fromCt = extensionFromContentType(contentType);
        if (fromCt != null) {
            return fromCt;
        }
        if ("video".equals(mediaType)) {
            return "mp4";
        }
        if ("audio".equals(mediaType)) {
            return "mp3";
        }
        if ("doc".equals(mediaType)) {
            return "pdf";
        }
        // if ("archive".equals(mediaType)) {
        //     return "zip";
        // }
        return "jpg";
    }

    private static String extensionFromContentType(String contentType) {
        if (!StringUtils.hasText(contentType)) {
            return null;
        }
        switch (contentType.toLowerCase(Locale.ROOT).trim()) {
            case "image/png":
                return "png";
            case "image/jpeg":
            case "image/jpg":
                return "jpg";
            case "image/webp":
                return "webp";
            case "image/gif":
                return "gif";
            case "video/mp4":
                return "mp4";
            case "video/quicktime":
                return "mov";
            case "video/webm":
                return "webm";
            case "video/x-msvideo":
                return "avi";
            case "video/mpeg":
                return "mpeg";
            case "audio/mpeg":
            case "audio/mp3":
                return "mp3";
            case "audio/mp4":
            case "audio/m4a":
            case "audio/x-m4a":
                return "m4a";
            case "audio/wav":
            case "audio/x-wav":
            case "audio/wave":
                return "wav";
            case "audio/aac":
                return "aac";
            case "audio/ogg":
                return "ogg";
            case "audio/webm":
                return "webm";
            case "audio/flac":
            case "audio/x-flac":
                return "flac";
            case "application/pdf":
                return "pdf";
            case "application/msword":
                return "doc";
            case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
                return "docx";
            case "application/vnd.ms-excel":
                return "xls";
            case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
                return "xlsx";
            case "application/vnd.ms-powerpoint":
                return "ppt";
            case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
                return "pptx";
            case "application/rtf":
            case "text/rtf":
                return "rtf";
            case "text/plain":
                return "txt";
            case "text/markdown":
                return "md";
            case "text/csv":
                return "csv";
            // archive 暂不开放。
            // case "application/zip":
            // case "application/x-zip-compressed":
            //     return "zip";
            // case "application/x-tar":
            //     return "tar";
            // case "application/gzip":
            // case "application/x-gzip":
            //     return "gz";
            // case "application/x-bzip2":
            //     return "bz2";
            // case "application/x-xz":
            //     return "xz";
            // case "application/x-7z-compressed":
            //     return "7z";
            // case "application/vnd.rar":
            // case "application/x-rar-compressed":
            //     return "rar";
            // case "application/zstd":
            // case "application/x-zstd":
            //     return "zst";
            default:
                return null;
        }
    }

    private static String defaultContentType(String mediaType, String extension) {
        if ("video".equals(mediaType)) {
            return "mp4".equals(extension) ? "video/mp4" : "video/" + extension;
        }
        if ("audio".equals(mediaType)) {
            if ("mp3".equals(extension) || "mpeg".equals(extension)) {
                return "audio/mpeg";
            }
            if ("m4a".equals(extension)) {
                return "audio/mp4";
            }
            if ("wav".equals(extension)) {
                return "audio/wav";
            }
            return "audio/" + extension;
        }
        if ("doc".equals(mediaType)) {
            if ("pdf".equals(extension)) {
                return "application/pdf";
            }
            if ("txt".equals(extension)) {
                return "text/plain";
            }
            return "application/octet-stream";
        }
        // if ("archive".equals(mediaType)) {
        //     return defaultArchiveContentType(extension);
        // }
        return "jpg".equals(extension) || "jpeg".equals(extension) ? "image/jpeg" : "image/" + extension;
    }

    // private static String defaultArchiveContentType(String extension) {
    //     if ("zip".equals(extension) || "zipx".equals(extension)) {
    //         return "application/zip";
    //     }
    //     if ("tar".equals(extension)) {
    //         return "application/x-tar";
    //     }
    //     if ("gz".equals(extension) || "tgz".equals(extension) || "tar.gz".equals(extension)) {
    //         return "application/gzip";
    //     }
    //     if ("bz2".equals(extension) || "tbz".equals(extension) || "tbz2".equals(extension)
    //             || "tar.bz2".equals(extension)) {
    //         return "application/x-bzip2";
    //     }
    //     if ("xz".equals(extension) || "txz".equals(extension) || "tar.xz".equals(extension)) {
    //         return "application/x-xz";
    //     }
    //     if ("7z".equals(extension)) {
    //         return "application/x-7z-compressed";
    //     }
    //     if ("rar".equals(extension)) {
    //         return "application/vnd.rar";
    //     }
    //     if ("zst".equals(extension) || "tzst".equals(extension) || "tar.zst".equals(extension)) {
    //         return "application/zstd";
    //     }
    //     return "application/octet-stream";
    // }

    private static boolean isImageExt(String ext) {
        return "png".equals(ext) || "jpg".equals(ext) || "jpeg".equals(ext)
                || "webp".equals(ext) || "gif".equals(ext);
    }

    private static boolean isVideoExt(String ext) {
        return "mp4".equals(ext) || "mov".equals(ext) || "webm".equals(ext)
                || "avi".equals(ext) || "mpeg".equals(ext) || "mpg".equals(ext);
    }

    private static boolean isAudioExt(String ext) {
        return "mp3".equals(ext) || "m4a".equals(ext) || "wav".equals(ext)
                || "aac".equals(ext) || "ogg".equals(ext) || "oga".equals(ext)
                || "flac".equals(ext) || "wma".equals(ext);
    }

    private static boolean isDocExt(String ext) {
        return "pdf".equals(ext) || "doc".equals(ext) || "docx".equals(ext)
                || "xls".equals(ext) || "xlsx".equals(ext)
                || "ppt".equals(ext) || "pptx".equals(ext)
                || "txt".equals(ext) || "md".equals(ext) || "csv".equals(ext)
                || "rtf".equals(ext);
    }

    // private static boolean isArchiveExt(String ext) {
    //     return "zip".equals(ext) || "zipx".equals(ext) || "tar".equals(ext)
    //             || "gz".equals(ext) || "tgz".equals(ext) || "tar.gz".equals(ext)
    //             || "bz2".equals(ext) || "tbz".equals(ext) || "tbz2".equals(ext)
    //             || "tar.bz2".equals(ext) || "xz".equals(ext) || "txz".equals(ext)
    //             || "tar.xz".equals(ext) || "7z".equals(ext) || "rar".equals(ext)
    //             || "zst".equals(ext) || "tzst".equals(ext) || "tar.zst".equals(ext);
    // }
}
