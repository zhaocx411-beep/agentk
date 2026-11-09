package com.kuaishou.agent.operation.backend.platform.service;

import java.util.Iterator;
import java.util.List;
import java.util.Map;

import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.fasterxml.jackson.databind.node.TextNode;
import com.kuaishou.agent.operation.backend.agentk.dto.AgentKMediaArtifactDto;
import com.kuaishou.agent.operation.backend.common.RegionHelper;
import com.kuaishou.agent.operation.backend.platform.support.BlobStoreOfficeUrls;

/**
 * 生产 agent-k 常返回 {@code http://bs3-hb1.internal/...}（IDC）。浏览器打不开，换成办公网域名。
 */
@Service
public class BlobStoreOfficeUrlService {

    static final String PROD_OFFICE_BASE = "https://bs3-hb1.corp.kuaishou.com";
    static final String STAGING_OFFICE_BASE = "https://bs3-hb1.staging.kuaishou.com";

    private final String officeHttpBase;

    public BlobStoreOfficeUrlService(
            @Value("${agent.operation.backend.media.office-http-base:}") String officeHttpBase) {
        this.officeHttpBase = StringUtils.hasText(officeHttpBase)
                ? officeHttpBase.trim()
                : defaultOfficeBase();
    }

    public String toOfficeUrl(String url) {
        return BlobStoreOfficeUrls.toOfficeUrl(url, officeHttpBase);
    }

    public void rewriteArtifacts(List<AgentKMediaArtifactDto> artifacts) {
        if (artifacts == null) {
            return;
        }
        for (AgentKMediaArtifactDto artifact : artifacts) {
            if (artifact != null) {
                artifact.setUrl(toOfficeUrl(artifact.getUrl()));
            }
        }
    }

    public void rewriteJson(JsonNode node) {
        if (node == null || node.isNull() || node.isMissingNode()) {
            return;
        }
        if (node.isTextual()) {
            return;
        }
        if (node.isArray()) {
            ArrayNode array = (ArrayNode) node;
            for (int i = 0; i < array.size(); i++) {
                JsonNode item = array.get(i);
                if (item != null && item.isTextual()) {
                    String rewritten = toOfficeUrl(item.asText());
                    if (!item.asText().equals(rewritten)) {
                        array.set(i, TextNode.valueOf(rewritten));
                    }
                } else {
                    rewriteJson(item);
                }
            }
            return;
        }
        if (!node.isObject()) {
            return;
        }
        ObjectNode object = (ObjectNode) node;
        Iterator<Map.Entry<String, JsonNode>> fields = object.fields();
        while (fields.hasNext()) {
            Map.Entry<String, JsonNode> field = fields.next();
            JsonNode value = field.getValue();
            if (value != null && value.isTextual()) {
                String rewritten = toOfficeUrl(value.asText());
                if (!value.asText().equals(rewritten)) {
                    object.put(field.getKey(), rewritten);
                }
            } else {
                rewriteJson(value);
            }
        }
    }

    private static String defaultOfficeBase() {
        return RegionHelper.isProdOrRegressionTest() ? PROD_OFFICE_BASE : STAGING_OFFICE_BASE;
    }
}
