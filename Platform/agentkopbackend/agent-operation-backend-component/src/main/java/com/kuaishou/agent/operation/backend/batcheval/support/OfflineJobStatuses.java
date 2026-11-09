package com.kuaishou.agent.operation.backend.batcheval.support;

/**
 * {@code batcheval_offline_job.status} 取值。
 */
public final class OfflineJobStatuses {

    public static final String PENDING = "PENDING";
    public static final String RUNNING = "RUNNING";
    public static final String SUCCEEDED = "SUCCEEDED";
    public static final String FAILED = "FAILED";
    /**
     * 用户已删除，子表还没清完。
     * <p>
     * 主表行本身就是「尚未清完」的标记：清理器按子表→主表的顺序删，主表消失才算结束，
     * 所以中途进程挂掉也能靠这个状态续跑。列表与详情一律当它不存在。
     */
    public static final String DELETING = "DELETING";

    private OfflineJobStatuses() {
    }

    public static boolean isFinished(String status) {
        return SUCCEEDED.equals(status) || FAILED.equals(status);
    }

    public static boolean isDeleting(String status) {
        return DELETING.equals(status);
    }
}
