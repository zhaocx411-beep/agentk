package com.kuaishou.agent.operation.backend.platform.service;

import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.platform.dto.ChatAssetDetailView;
import com.kuaishou.agent.operation.backend.platform.dto.ChatAssetPortfolioItemView;
import com.kuaishou.agent.operation.backend.platform.dto.ChatAssetPortfolioItemView.AssetBrief;
import com.kuaishou.agent.operation.backend.platform.dto.ChatAssetPortfolioQuery;
import com.kuaishou.agent.operation.backend.platform.dto.PageResult;
import com.kuaishou.agent.operation.backend.platform.entity.ChatAsset;
import com.kuaishou.agent.operation.backend.platform.entity.ChatSession;
import com.kuaishou.agent.operation.backend.platform.entity.PipelineExec;
import com.kuaishou.agent.operation.backend.platform.service.ChatPersistService.ChatAssetPageQuery;

@Service
public class ChatAssetPortfolioService {

    private static final Logger LOGGER = LoggerFactory.getLogger(ChatAssetPortfolioService.class);

    /** 聚合扫描上限，避免一次拉全表。 */
    private static final int PORTFOLIO_GROUP_SCAN_LIMIT = 2000;

    private final ChatPersistService chatPersistService;
    private final ObjectMapper objectMapper;
    private final BlobStoreUrlService blobStoreUrlService;
    private final BlobStoreOfficeUrlService officeUrlService;

    public ChatAssetPortfolioService(ChatPersistService chatPersistService, ObjectMapper objectMapper,
            BlobStoreUrlService blobStoreUrlService, BlobStoreOfficeUrlService officeUrlService) {
        this.chatPersistService = chatPersistService;
        this.objectMapper = objectMapper;
        this.blobStoreUrlService = blobStoreUrlService;
        this.officeUrlService = officeUrlService;
    }

    /**
     * 作品集分页：按 run / pipeline / api 执行（{@link #roundKey}）聚合；
     * 每条记录含中间产物与结果产物。
     * <p>
     * 团队/公开展示：{@code query.userId} 可选；不传则不按用户过滤。
     */
    public PageResult<ChatAssetPortfolioItemView> query(ChatAssetPortfolioQuery query) {
        return queryInternal(query);
    }

    /**
     * 我的作品集：强制按 {@code userId} 过滤，忽略 body 中的 userId。
     */
    public PageResult<ChatAssetPortfolioItemView> queryMine(String userId, ChatAssetPortfolioQuery query) {
        if (!StringUtils.hasText(userId)) {
            throw new IllegalArgumentException("userId required for my portfolio");
        }
        ChatAssetPortfolioQuery q = query == null ? new ChatAssetPortfolioQuery() : query;
        q.setUserId(userId.trim());
        return queryInternal(q);
    }

    private PageResult<ChatAssetPortfolioItemView> queryInternal(ChatAssetPortfolioQuery query) {
        ChatAssetPortfolioQuery q = query == null ? new ChatAssetPortfolioQuery() : query;
        long pageNo = q.getPage() == null || q.getPage() <= 0 ? 1L : q.getPage().longValue();
        long pageSize = q.getPageSize() == null || q.getPageSize() <= 0
                ? 20L : Math.min(q.getPageSize().longValue(), 100L);

        List<ChatAsset> scanned = chatPersistService.listAssetsForPortfolioGrouping(
                toPageQuery(q), PORTFOLIO_GROUP_SCAN_LIMIT);
        LinkedHashMap<String, List<ChatAsset>> groups = groupByRoundKey(scanned);
        List<String> roundKeys = new ArrayList<>(groups.keySet());
        long total = roundKeys.size();
        long fromLong;
        try {
            fromLong = Math.multiplyExact(pageNo - 1, pageSize);
        } catch (ArithmeticException e) {
            fromLong = total;
        }
        fromLong = Math.min(Math.max(fromLong, 0L), total);
        long toLong = Math.min(fromLong + pageSize, total);
        if (fromLong > Integer.MAX_VALUE || toLong > Integer.MAX_VALUE) {
            return PageResult.of(pageNo, pageSize, total, Collections.emptyList());
        }
        int from = (int) fromLong;
        int to = (int) toLong;
        List<String> pageKeys = roundKeys.subList(from, to);

        List<Long> sessionIds = new ArrayList<>();
        List<ChatAssetPortfolioItemView> records = new ArrayList<>();
        for (String key : pageKeys) {
            List<ChatAsset> roundAssets = groups.get(key);
            if (roundAssets == null || roundAssets.isEmpty()) {
                continue;
            }
            ChatAsset head = roundAssets.get(0);
            if (head.getSessionId() != null) {
                sessionIds.add(head.getSessionId());
            }
        }
        Map<Long, ChatSession> sessionById = chatPersistService.mapSessionsByIds(sessionIds);
        Map<String, PipelineExec> execByTaskId = loadApiPipelineExecs(groups, pageKeys);
        Map<String, Integer> blobOccurrence = new HashMap<>();
        for (String key : pageKeys) {
            List<ChatAsset> roundAssets = groups.get(key);
            if (roundAssets == null || roundAssets.isEmpty()) {
                continue;
            }
            List<ChatAsset> chronological = new ArrayList<>(roundAssets);
            chronological.sort(Comparator
                    .comparing((ChatAsset a) -> a.getAgentkEventSeq() == null ? Integer.MAX_VALUE : a.getAgentkEventSeq())
                    .thenComparing(a -> a.getId() == null ? Long.MAX_VALUE : a.getId()));
            ChatAsset head = chronological.get(chronological.size() - 1);
            records.add(toRoundView(key, chronological, sessionById.get(head.getSessionId()),
                    execByTaskId, blobOccurrence));
        }
        return PageResult.of(pageNo, pageSize, total, records);
    }

    private ChatAssetPageQuery toPageQuery(ChatAssetPortfolioQuery query) {
        ChatAssetPageQuery pageQuery = new ChatAssetPageQuery();
        pageQuery.setUserId(query.getUserId());
        pageQuery.setAssetType(query.getAssetType());
        pageQuery.setSourceKind(query.getSourceKind());
        pageQuery.setSource(query.getSource());
        pageQuery.setSessionId(query.getSessionId());
        pageQuery.setRunId(query.getRunId());
        pageQuery.setAgentkRunId(query.getAgentkRunId());
        pageQuery.setWorkflowExecId(query.getWorkflowExecId());
        pageQuery.setPromptKeyword(query.getPromptKeyword());
        pageQuery.setAgentName(query.getAgentName());
        pageQuery.setWorkflowName(query.getWorkflowName());
        if (query.getPage() != null && query.getPage() > 0) {
            pageQuery.setPage(query.getPage());
        }
        if (query.getPageSize() != null && query.getPageSize() > 0) {
            pageQuery.setPageSize(query.getPageSize());
        }
        return pageQuery;
    }

    public ChatAssetDetailView detail(long assetId) {
        ChatAsset asset = chatPersistService.requireAsset(assetId);
        ChatSession session = findPortfolioSession(asset);
        List<ChatAsset> sameRound = chatPersistService.listAssetsInSameRound(asset);
        return toDetailView(asset, session, sameRound, resolveRoundBlobUrls(sameRound, asset));
    }

    /**
     * 我的作品详情：必须是当前用户资产；展示规则与公开展示一致（synced + image/video）。
     */
    public ChatAssetDetailView detailMine(long assetId, String userId) {
        if (!StringUtils.hasText(userId)) {
            throw new IllegalArgumentException("userId required for my portfolio detail");
        }
        ChatAsset owned = chatPersistService.requireOwnedAsset(assetId, userId.trim());
        // 再走公开可见性校验（synced / 类型 / 失败 run 排除），避免脏数据进详情
        ChatAsset asset = chatPersistService.requireAsset(owned.getId());
        ChatSession session = findPortfolioSession(asset);
        List<ChatAsset> sameRound = chatPersistService.listAssetsInSameRound(asset);
        return toDetailView(asset, session, sameRound, resolveRoundBlobUrls(sameRound, asset));
    }

    /**
     * api 独立任务把 taskId hash 成 sessionId 落资产，并不写 aop_chat_session。
     * 列表用 {@code mapSessionsByIds} 找不到就空着；详情不能 {@code requireSession} 直接 400。
     */
    private ChatSession findPortfolioSession(ChatAsset asset) {
        if (asset == null || asset.getSessionId() == null) {
            return null;
        }
        return chatPersistService.findById(asset.getSessionId()).orElse(null);
    }

    /** 团队/公开作品集：public 智能体、public Workflow、API taskType 去重（全站）。 */
    public List<String> listAgentNames() {
        return chatPersistService.listBoundAgentNames(null, true);
    }

    /** 我的作品集：当前用户 {@code aop_asset.agent_name} 去重（含本人 private 智能体）。 */
    public List<String> listAgentNamesMine(String userId) {
        if (!StringUtils.hasText(userId)) {
            throw new IllegalArgumentException("userId required for my portfolio agent names");
        }
        return chatPersistService.listBoundAgentNames(userId.trim(), false);
    }

    /** 团队/公开作品集：有作品且 session_kind=workflow 的 Workflow 名列表。 */
    public List<String> listWorkflowNames() {
        return chatPersistService.listBoundWorkflowNames(null);
    }

    /** 我的作品集：当前用户有作品且 session_kind=workflow 的 Workflow 名列表。 */
    public List<String> listWorkflowNamesMine(String userId) {
        if (!StringUtils.hasText(userId)) {
            throw new IllegalArgumentException("userId required for my portfolio workflow names");
        }
        return chatPersistService.listBoundWorkflowNames(userId.trim());
    }

    private LinkedHashMap<String, List<ChatAsset>> groupByRoundKey(List<ChatAsset> scanned) {
        LinkedHashMap<String, List<ChatAsset>> groups = new LinkedHashMap<>();
        if (scanned == null) {
            return groups;
        }
        for (ChatAsset asset : scanned) {
            if (asset == null) {
                continue;
            }
            String key = roundKey(asset);
            groups.computeIfAbsent(key, ignored -> new ArrayList<>()).add(asset);
        }
        return groups;
    }

    private ChatAssetPortfolioItemView toRoundView(String roundKey, List<ChatAsset> chronological,
            ChatSession session, Map<String, PipelineExec> execByTaskId,
            Map<String, Integer> blobOccurrence) {
        ChatAssetPortfolioItemView view = new ChatAssetPortfolioItemView();
        view.setRoundKey(roundKey);
        ChatAsset any = chronological.get(0);

        List<ChatAsset> intermediate = new ArrayList<>();
        List<ChatAsset> result = new ArrayList<>();
        splitIntermediateAndResult(chronological, intermediate, result);

        Map<Long, String> blobUrls = resolveRoundBlobUrls(chronological, execByTaskId, blobOccurrence);
        view.setIntermediateAssets(toBriefs(intermediate, blobUrls));
        view.setResultAssets(toBriefs(result, blobUrls));

        // cover 取结果产物中时间序最后一个（全 video 时即最后一条成片）
        ChatAsset coverSource = !result.isEmpty()
                ? result.get(result.size() - 1)
                : (!intermediate.isEmpty() ? intermediate.get(intermediate.size() - 1) : any);
        view.setCover(toBrief(coverSource, blobUrls));
        view.setPromptText(resolvePrompt(result, intermediate, any));
        view.setCreatedAt(coverSource.getCreatedAt() != null ? coverSource.getCreatedAt() : any.getCreatedAt());

        ChatAsset meta = coverSource;
        view.setRunId(meta.getRunId());
        view.setAgentkRunId(meta.getAgentkRunId());
        view.setWorkflowExecId(meta.getWorkflowExecId());
        view.setSourceKind(meta.getSourceKind());
        view.setSource(meta.getSource());
        view.setSourceName(meta.getSourceName());
        view.setAgentName(resolveDisplayAgentName(meta, session));
        view.setWorkflowName(resolveDisplayWorkflowName(session));
        view.setUserId(meta.getUserId());

        if (session != null) {
            ChatAssetPortfolioItemView.SessionSummary summary = new ChatAssetPortfolioItemView.SessionSummary();
            summary.setSessionId(session.getId());
            summary.setPublicId(session.getPublicId());
            summary.setAgentkSessionId(session.getAgentkSessionId());
            summary.setTitle(session.getTitle());
            summary.setSessionKind(session.getSessionKind());
            summary.setEffectiveModel(session.getEffectiveModel());
            summary.setAgentLoopVersionId(session.getAgentVersionId());
            summary.setWorkflowVersionId(session.getWorkflowVersionId());
            summary.setSessionCreatedAt(session.getCreatedAt());
            view.setSession(summary);
        }
        return view;
    }

    /** 仅智能体名；有 session 时以联查为准，避免历史 agent_name 混入 workflow 名。 */
    private String resolveDisplayAgentName(ChatAsset asset, ChatSession session) {
        String fromSession = chatPersistService.resolveSessionAgentName(session);
        if (StringUtils.hasText(fromSession)) {
            return fromSession.trim();
        }
        if (asset != null && StringUtils.hasText(asset.getAgentName())) {
            return asset.getAgentName().trim();
        }
        return "";
    }

    /** 仅 session_kind=workflow 时返回 workflow 名；否则空串。 */
    private String resolveDisplayWorkflowName(ChatSession session) {
        String name = chatPersistService.resolveSessionWorkflowName(session);
        return StringUtils.hasText(name) ? name.trim() : "";
    }

    static void splitIntermediateAndResult(List<ChatAsset> chronological,
            List<ChatAsset> intermediate, List<ChatAsset> result) {
        if (chronological == null || chronological.isEmpty()) {
            return;
        }
        boolean hasVideo = false;
        boolean hasImage = false;
        for (ChatAsset asset : chronological) {
            if ("video".equals(asset.getAssetType())) {
                hasVideo = true;
            } else if ("image".equals(asset.getAssetType())) {
                hasImage = true;
            }
        }
        if (hasVideo && hasImage) {
            for (ChatAsset asset : chronological) {
                if ("video".equals(asset.getAssetType())) {
                    result.add(asset);
                } else {
                    intermediate.add(asset);
                }
            }
            return;
        }
        if (chronological.size() == 1) {
            result.add(chronological.get(0));
            return;
        }
        intermediate.addAll(chronological.subList(0, chronological.size() - 1));
        result.add(chronological.get(chronological.size() - 1));
    }

    private static String resolvePrompt(List<ChatAsset> result, List<ChatAsset> intermediate, ChatAsset fallback) {
        for (int i = result.size() - 1; i >= 0; i--) {
            if (StringUtils.hasText(result.get(i).getPromptText())) {
                return result.get(i).getPromptText();
            }
        }
        for (int i = intermediate.size() - 1; i >= 0; i--) {
            if (StringUtils.hasText(intermediate.get(i).getPromptText())) {
                return intermediate.get(i).getPromptText();
            }
        }
        return fallback == null ? null : fallback.getPromptText();
    }

    private List<AssetBrief> toBriefs(List<ChatAsset> assets, Map<Long, String> blobUrls) {
        List<AssetBrief> briefs = new ArrayList<>();
        if (assets == null) {
            return briefs;
        }
        for (ChatAsset asset : assets) {
            briefs.add(toBrief(asset, blobUrls));
        }
        return briefs;
    }

    private AssetBrief toBrief(ChatAsset asset, Map<Long, String> blobUrls) {
        AssetBrief brief = new AssetBrief();
        if (asset == null) {
            return brief;
        }
        brief.setAssetId(asset.getId());
        brief.setAssetType(asset.getAssetType());
        brief.setAssetUrl(liveAssetUrl(asset, blobUrls));
        brief.setPromptText(asset.getPromptText());
        brief.setAgentkEventSeq(asset.getAgentkEventSeq());
        brief.setCreatedAt(asset.getCreatedAt());
        return brief;
    }

    private ChatAssetDetailView toDetailView(ChatAsset asset, ChatSession session,
            List<ChatAsset> sameRound, Map<Long, String> blobUrls) {
        ChatAssetDetailView view = new ChatAssetDetailView();
        view.setAssetId(asset.getId());
        view.setAssetType(asset.getAssetType());
        view.setAssetUrl(liveAssetUrl(asset, blobUrls));
        view.setPromptText(asset.getPromptText());
        view.setSourceKind(asset.getSourceKind());
        view.setSource(asset.getSource());
        view.setSourceName(asset.getSourceName());
        view.setAgentName(resolveDisplayAgentName(asset, session));
        view.setWorkflowName(resolveDisplayWorkflowName(session));
        view.setWorkflowExecId(asset.getWorkflowExecId());
        view.setAgentkRunId(asset.getAgentkRunId());
        view.setAgentkEventSeq(asset.getAgentkEventSeq());
        view.setRunId(asset.getRunId());
        view.setRoundKey(roundKey(asset));
        view.setMessageId(asset.getMessageId());
        view.setUserId(asset.getUserId());
        view.setStatus(asset.getStatus());
        view.setCreatedAt(asset.getCreatedAt());
        view.setUpdatedAt(asset.getUpdatedAt());
        view.setSameRoundAssets(toSameRoundSummaries(sameRound, blobUrls));

        if (session != null) {
            ChatAssetDetailView.SessionContext ctx = new ChatAssetDetailView.SessionContext();
            ctx.setSessionId(session.getId());
            ctx.setPublicId(session.getPublicId());
            ctx.setAgentkSessionId(session.getAgentkSessionId());
            ctx.setTitle(session.getTitle());
            ctx.setSessionKind(session.getSessionKind());
            ctx.setEntrySource(session.getEntrySource());
            ctx.setStatus(session.getStatus());
            ctx.setCreatedAt(session.getCreatedAt());
            view.setSession(ctx);
            view.setProfile(parseSessionProfile(session.getProfileYaml()));
        }
        return view;
    }

    /** 会话建连时固化的 profile 快照；解析失败返回 null。 */
    private JsonNode parseSessionProfile(String profileYamlOrJson) {
        if (!StringUtils.hasText(profileYamlOrJson)) {
            return null;
        }
        try {
            JsonNode node = objectMapper.readTree(profileYamlOrJson.trim());
            return node != null && node.isObject() ? node : null;
        } catch (Exception e) {
            LOGGER.warn("parse session profileYaml failed: {}", e.getMessage());
            return null;
        }
    }

    private List<ChatAssetDetailView.SameRoundAssetSummary> toSameRoundSummaries(
            List<ChatAsset> assets, Map<Long, String> blobUrls) {
        List<ChatAssetDetailView.SameRoundAssetSummary> summaries = new ArrayList<>();
        if (assets == null) {
            return summaries;
        }
        for (ChatAsset item : assets) {
            if (item == null) {
                continue;
            }
            ChatAssetDetailView.SameRoundAssetSummary summary = new ChatAssetDetailView.SameRoundAssetSummary();
            summary.setAssetId(item.getId());
            summary.setAssetType(item.getAssetType());
            summary.setAssetUrl(liveAssetUrl(item, blobUrls));
            summary.setAgentkEventSeq(item.getAgentkEventSeq());
            summaries.add(summary);
        }
        return summaries;
    }

    private Map<String, PipelineExec> loadApiPipelineExecs(
            LinkedHashMap<String, List<ChatAsset>> groups, List<String> pageKeys) {
        List<ChatAsset> candidates = new ArrayList<>();
        for (String key : pageKeys) {
            List<ChatAsset> assets = groups.get(key);
            if (assets == null) {
                continue;
            }
            for (ChatAsset asset : assets) {
                if (isApiAsset(asset) && StringUtils.hasText(asset.getWorkflowExecId())) {
                    candidates.add(asset);
                }
            }
        }
        return indexPipelineExecs(staleApiExecIds(candidates));
    }

    private Map<Long, String> resolveRoundBlobUrls(List<ChatAsset> sameRound, ChatAsset fallback) {
        List<ChatAsset> assets = new ArrayList<>();
        if (sameRound != null) {
            assets.addAll(sameRound);
        }
        if (fallback != null && (sameRound == null || sameRound.stream().noneMatch(a ->
                a != null && fallback.getId() != null && fallback.getId().equals(a.getId())))) {
            assets.add(fallback);
        }
        List<ChatAsset> toCheck = new ArrayList<>();
        for (ChatAsset asset : assets) {
            if (isApiAsset(asset) && StringUtils.hasText(asset.getWorkflowExecId())) {
                toCheck.add(asset);
            }
        }
        return resolveRoundBlobUrls(assets, indexPipelineExecs(staleApiExecIds(toCheck)), new HashMap<>());
    }

    /** 与原先循环条件相同：api 资产且 URL 已不 fresh，才去拉 pipeline exec。 */
    private List<String> staleApiExecIds(List<ChatAsset> candidates) {
        List<String> execIds = new ArrayList<>();
        if (candidates == null) {
            return execIds;
        }
        for (ChatAsset asset : candidates) {
            if (asset != null && !blobStoreUrlService.stillFresh(asset.getAssetUrl(), asset.getUpdatedAt())) {
                execIds.add(asset.getWorkflowExecId().trim());
            }
        }
        return execIds;
    }

    private Map<Long, String> resolveRoundBlobUrls(List<ChatAsset> chronological,
            Map<String, PipelineExec> execByTaskId, Map<String, Integer> blobOccurrence) {
        Map<Long, String> urls = new HashMap<>();
        if (chronological == null) {
            return urls;
        }
        Map<Long, String> toRefresh = new LinkedHashMap<>();
        for (ChatAsset asset : chronological) {
            if (asset == null || asset.getId() == null) {
                continue;
            }
            urls.put(asset.getId(), resolveLiveBlobUrl(asset, execByTaskId, blobOccurrence, toRefresh));
        }
        persistRefreshedUrls(toRefresh, chronological);
        return urls;
    }

    private String resolveLiveBlobUrl(ChatAsset asset, Map<String, PipelineExec> execByTaskId,
            Map<String, Integer> blobOccurrence, Map<Long, String> toRefresh) {
        String fallback = asset.getAssetUrl();
        if (!isApiAsset(asset) || !StringUtils.hasText(asset.getWorkflowExecId())) {
            return fallback;
        }
        if (blobStoreUrlService.stillFresh(fallback, asset.getUpdatedAt())) {
            return fallback;
        }
        PipelineExec exec = execByTaskId.get(asset.getWorkflowExecId().trim());
        if (exec == null || !StringUtils.hasText(exec.getOutputJson())) {
            return fallback;
        }
        String occKey = asset.getWorkflowExecId().trim() + "\n"
                + (asset.getSourceName() == null ? "" : asset.getSourceName().trim());
        int occurrence = blobOccurrence.getOrDefault(occKey, 0);
        blobOccurrence.put(occKey, occurrence + 1);
        String resolved = blobStoreUrlService.resolveFromOutput(
                exec.getOutputJson(), asset.getSourceName(), occurrence);
        if (!StringUtils.hasText(resolved)) {
            return fallback;
        }
        toRefresh.put(asset.getId(), resolved);
        return resolved;
    }

    private void persistRefreshedUrls(Map<Long, String> toRefresh, List<ChatAsset> chronological) {
        if (toRefresh == null || toRefresh.isEmpty()) {
            return;
        }
        LocalDateTime now = LocalDateTime.now();
        try {
            chatPersistService.batchRefreshAssetUrls(toRefresh);
            for (ChatAsset asset : chronological) {
                if (asset == null || asset.getId() == null) {
                    continue;
                }
                String url = toRefresh.get(asset.getId());
                if (StringUtils.hasText(url)) {
                    asset.setAssetUrl(url);
                    asset.setUpdatedAt(now);
                }
            }
        } catch (Exception e) {
            LOGGER.warn("batch refresh asset urls failed size={}: {}", toRefresh.size(), e.getMessage());
        }
    }

    private Map<String, PipelineExec> indexPipelineExecs(List<String> execIds) {
        Map<String, PipelineExec> byId = new HashMap<>();
        List<PipelineExec> rows = chatPersistService.listPipelineExecsByAgentkExecIds(execIds);
        if (rows == null) {
            return byId;
        }
        for (PipelineExec row : rows) {
            if (row != null && StringUtils.hasText(row.getAgentkExecId())) {
                byId.putIfAbsent(row.getAgentkExecId().trim(), row);
            }
        }
        return byId;
    }

    private String liveAssetUrl(ChatAsset asset, Map<Long, String> blobUrls) {
        if (asset == null) {
            return null;
        }
        if (blobUrls != null && asset.getId() != null && blobUrls.containsKey(asset.getId())) {
            String live = blobUrls.get(asset.getId());
            if (StringUtils.hasText(live)) {
                return officeUrlService.toOfficeUrl(live);
            }
        }
        return officeUrlService.toOfficeUrl(asset.getAssetUrl());
    }

    private static boolean isApiAsset(ChatAsset asset) {
        return asset != null && "api".equals(asset.getSourceKind() == null ? "" : asset.getSourceKind().trim());
    }

    static String roundKey(ChatAsset asset) {
        if (asset == null) {
            return "";
        }
        // pipeline / api 都把 workflowExecId 写成同一次执行 id；不能只认 pipeline，
        // 否则 api 会落到 session:…:asset:{id}，作品集按资产散开。
        if (groupsByWorkflowExec(asset)) {
            return asset.getSourceKind().trim() + ":" + asset.getWorkflowExecId().trim();
        }
        if (StringUtils.hasText(asset.getAgentkRunId())) {
            return "round:" + asset.getAgentkRunId().trim();
        }
        if (asset.getRunId() != null && asset.getRunId() > 0) {
            return "round:platform:" + asset.getRunId();
        }
        return "session:" + asset.getSessionId() + ":asset:" + asset.getId();
    }

    /** pipeline / api / capability 按 {@code aop_asset.agentk_pipeline_exec_id} 聚成一轮。 */
    public static boolean groupsByWorkflowExec(ChatAsset asset) {
        if (asset == null || !StringUtils.hasText(asset.getWorkflowExecId())) {
            return false;
        }
        String kind = asset.getSourceKind() == null ? "" : asset.getSourceKind().trim();
        return "pipeline".equals(kind) || "api".equals(kind) || "capability".equals(kind);
    }
}
