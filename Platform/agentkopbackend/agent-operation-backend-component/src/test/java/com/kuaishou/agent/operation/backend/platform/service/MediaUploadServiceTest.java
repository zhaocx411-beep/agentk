package com.kuaishou.agent.operation.backend.platform.service;

import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.lang.reflect.Field;
import java.net.URL;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.web.multipart.MultipartFile;

import com.amazonaws.services.s3.model.ObjectMetadata;
import com.kuaishou.agent.operation.backend.platform.dao.mapper.MediaAttachmentMapper;
import com.kuaishou.agent.operation.backend.platform.entity.MediaAttachment;
import com.kuaishou.blobstore.s3.BS3Client;

class MediaUploadServiceTest {

    private BS3Client bs3Client;
    private MediaAttachmentMapper mediaAttachmentMapper;
    private MediaUploadService service;

    @BeforeEach
    void setUp() throws Exception {
        bs3Client = mock(BS3Client.class);
        mediaAttachmentMapper = mock(MediaAttachmentMapper.class);
        service = new MediaUploadService(bs3Client, mediaAttachmentMapper);
        setField("bucketName", "bucket");
        setField("keyPrefix", "media/");
        setField("maxImageBytes", 1024L);
        setField("maxVideoBytes", 1024L);
        setField("maxAudioBytes", 1024L);
        setField("maxDocBytes", 1024L);
        setField("maxBatchFiles", 2);

        when(bs3Client.getUrl(anyString(), anyString())).thenReturn(new URL("https://blob.example/file"));
        when(bs3Client.generateCdnUri(anyString(), anyString())).thenReturn("https://cdn.example/file");
        when(bs3Client.getObjectMetadata(anyString(), anyString())).thenReturn(new ObjectMetadata());
        when(mediaAttachmentMapper.insert(any(MediaAttachment.class))).thenAnswer(invocation -> {
            MediaAttachment row = invocation.getArgument(0);
            row.setId(1L);
            return 1;
        });
    }

    @Test
    void shouldRejectArchiveFormatsWhileArchiveUploadDisabled() {
        assertThatThrownBy(() -> service.upload(file("demo.zip"), null, "alice"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("无法识别媒体类型");

        verify(bs3Client, never()).putObject(eq("bucket"), anyString(), any(InputStream.class),
                any(ObjectMetadata.class));
    }

    @Test
    void shouldRejectBatchUploadWhenFileCountExceedsLimitBeforeUploading() {
        MultipartFile[] files = {
                file("a.pdf"), file("b.txt"), file("c.csv")
        };

        assertThatThrownBy(() -> service.uploadBatch(files, null, "alice"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("批量上传文件数量不能超过 2 个");

        verify(bs3Client, never()).putObject(eq("bucket"), anyString(), any(InputStream.class),
                any(ObjectMetadata.class));
        verify(mediaAttachmentMapper, never()).insert(any(MediaAttachment.class));
    }

    private MultipartFile file(String filename) {
        return new MultipartFile() {
            private final byte[] bytes = "archive".getBytes(java.nio.charset.StandardCharsets.UTF_8);

            @Override
            public String getName() {
                return "file";
            }

            @Override
            public String getOriginalFilename() {
                return filename;
            }

            @Override
            public String getContentType() {
                return null;
            }

            @Override
            public boolean isEmpty() {
                return false;
            }

            @Override
            public long getSize() {
                return bytes.length;
            }

            @Override
            public byte[] getBytes() {
                return bytes.clone();
            }

            @Override
            public InputStream getInputStream() {
                return new ByteArrayInputStream(bytes);
            }

            @Override
            public void transferTo(java.io.File dest) throws IOException {
                throw new UnsupportedOperationException("not needed");
            }
        };
    }

    private void setField(String name, Object value) throws Exception {
        Field field = MediaUploadService.class.getDeclaredField(name);
        field.setAccessible(true);
        field.set(service, value);
    }
}
