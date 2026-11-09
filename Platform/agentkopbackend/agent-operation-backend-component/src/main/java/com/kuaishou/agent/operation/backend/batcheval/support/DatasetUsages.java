package com.kuaishou.agent.operation.backend.batcheval.support;

/**
 * 数据集用途：同一张 {@code batcheval_dataset} 表按 usage 分流。
 * <ul>
 *   <li>{@link #EVAL}：离线跑数数据集，离线跑数页只列这一类</li>
 *   <li>{@link #TEMPLATE}：pipeline 入口的素材/模版，配合 {@code pipeline_name} 筛选</li>
 * </ul>
 * pipeline_name 只服务模版筛选：离线跑数即使走 pipeline 模式，用的仍是 EVAL 数据集。
 */
public final class DatasetUsages {

    public static final String EVAL = "EVAL";
    public static final String TEMPLATE = "TEMPLATE";

    private DatasetUsages() {
    }

    /** 上传必填。空值不再默认 EVAL。 */
    public static String require(String usage) {
        if (usage == null || usage.trim().isEmpty()) {
            throw new IllegalArgumentException("usage 不能为空，仅支持 EVAL(离线跑数) / TEMPLATE(模版/素材)");
        }
        return parse(usage);
    }

    /** 列表筛选与存量数据：空值按 EVAL。同时接受中文与英文写法。 */
    public static String normalize(String usage) {
        if (usage == null || usage.trim().isEmpty()) {
            return EVAL;
        }
        return parse(usage);
    }

    private static String parse(String usage) {
        String value = usage.trim();
        if (TEMPLATE.equalsIgnoreCase(value)
                || "模版".equals(value)
                || "模板".equals(value)
                || "素材".equals(value)) {
            return TEMPLATE;
        }
        if (EVAL.equalsIgnoreCase(value) || "离线跑数".equals(value)) {
            return EVAL;
        }
        throw new IllegalArgumentException(
                "usage 仅支持 EVAL(离线跑数) / TEMPLATE(模版/素材)，当前值: " + usage);
    }

    public static boolean isTemplate(String usage) {
        return TEMPLATE.equals(normalize(usage));
    }
}
