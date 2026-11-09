package com.kuaishou.agent.operation.backend.keffects;

import org.springframework.stereotype.Service;

import com.fasterxml.jackson.databind.JsonNode;
import com.kuaishou.agent.operation.backend.keffects.dto.KEffectsCreateTaskRequest;
import com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskAssetService;
import com.kuaishou.agent.operation.backend.keffects.task.KEffectsTaskAssetService.AssetProfile;
import com.kuaishou.agent.operation.backend.keffects.task.adapter.ClothingDupeTaskAdapter;

/**
 * @deprecated 保留给旧调用方；新 taskType 统一通过 {@link KEffectsTaskAssetService}。
 */
@Deprecated
@Service
public class KEffectsClothingDupeAssetService {

    public static final String SOURCE_KIND = KEffectsTaskAssetService.SOURCE_KIND;
    public static final String TASK_TYPE = ClothingDupeTaskAdapter.TASK_TYPE;

    private static final AssetProfile ASSET_PROFILE = AssetProfile.of(TASK_TYPE,
            "productVideo", "videoWithBgmTts", "stitchedVideo",
            "videoWithBgm", "videoWithTts", "videoRaw");

    private final KEffectsTaskAssetService delegate;

    public KEffectsClothingDupeAssetService(KEffectsTaskAssetService delegate) {
        this.delegate = delegate;
    }

    public int persistAfterCreate(JsonNode upstream, KEffectsCreateTaskRequest request, String userId) {
        return delegate.persistAfterCreate(upstream, request, userId, ASSET_PROFILE);
    }

    public int persistAfterGet(JsonNode upstream, String pathTaskId, String userId) {
        return delegate.persistAfterGet(upstream, pathTaskId, userId, ASSET_PROFILE);
    }

    public JsonNode enrichWithBlobUrls(JsonNode upstream) {
        return delegate.enrichWithBlobUrls(upstream);
    }
}
