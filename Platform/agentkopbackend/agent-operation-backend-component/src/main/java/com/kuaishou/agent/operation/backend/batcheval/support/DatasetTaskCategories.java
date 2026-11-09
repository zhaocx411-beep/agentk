package com.kuaishou.agent.operation.backend.batcheval.support;

import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.Locale;
import java.util.Map;
import java.util.Set;

/**
 * 用例类目，落在 {@code batcheval_dataset_task.category}。
 * 空值表示未标注；已知中文展示名仍映射到素材 code，其余原样落库。
 * 跑数 / 素材用数据集 {@code usage} 区分，不在这里限制取值。
 */
public final class DatasetTaskCategories {

    public static final int MAX_LENGTH = 128;

    public static final String PRODUCT_IMAGE = "product_image";
    public static final String MODEL_IMAGE = "model_image";
    public static final String STYLE_REF_IMAGE = "style_ref_image";
    public static final String AUDIO = "audio";
    public static final String ECOMMERCE_AD = "ecommerce_ad";
    public static final String RUNWAY = "runway";
    public static final String SHORT_DRAMA = "short_drama";
    public static final String BROLL = "broll";

    private static final Set<String> ALLOWED = Collections.unmodifiableSet(new LinkedHashSet<>(Arrays.asList(
            PRODUCT_IMAGE,
            MODEL_IMAGE,
            STYLE_REF_IMAGE,
            AUDIO,
            ECOMMERCE_AD,
            RUNWAY,
            SHORT_DRAMA,
            BROLL)));

    private static final Map<String, String> ALIASES;

    static {
        Map<String, String> aliases = new LinkedHashMap<>();
        aliases.put("商品图", PRODUCT_IMAGE);
        aliases.put("模特图", MODEL_IMAGE);
        aliases.put("风格参考图", STYLE_REF_IMAGE);
        aliases.put("场景参考图", STYLE_REF_IMAGE);
        aliases.put("风格/场景参考图", STYLE_REF_IMAGE);
        aliases.put("音频", AUDIO);
        aliases.put("电商广告", ECOMMERCE_AD);
        aliases.put("商品广告", ECOMMERCE_AD);
        aliases.put("电商广告成片", ECOMMERCE_AD);
        aliases.put("走秀", RUNWAY);
        aliases.put("试穿", RUNWAY);
        aliases.put("走秀/试穿", RUNWAY);
        aliases.put("短剧", SHORT_DRAMA);
        aliases.put("漫剧", SHORT_DRAMA);
        aliases.put("短剧/漫剧", SHORT_DRAMA);
        aliases.put("空镜", BROLL);
        aliases.put("氛围", BROLL);
        aliases.put("空镜/氛围", BROLL);
        ALIASES = Collections.unmodifiableMap(aliases);
    }

    private DatasetTaskCategories() {
    }

    public static Set<String> allowed() {
        return ALLOWED;
    }

    /**
     * 空值保持 {@code null}；已知中文展示名映射到 code；已知英文 code 大小写不敏感；其余 trim 后原样落库。
     */
    public static String normalize(String category) {
        if (category == null) {
            return null;
        }
        String value = category.trim();
        if (value.isEmpty()) {
            return null;
        }
        if (value.length() > MAX_LENGTH) {
            throw new IllegalArgumentException("category 长度不能超过 " + MAX_LENGTH);
        }
        String aliased = ALIASES.get(value);
        if (aliased != null) {
            return aliased;
        }
        String code = value.toLowerCase(Locale.ROOT);
        if (ALLOWED.contains(code)) {
            return code;
        }
        return value;
    }
}
