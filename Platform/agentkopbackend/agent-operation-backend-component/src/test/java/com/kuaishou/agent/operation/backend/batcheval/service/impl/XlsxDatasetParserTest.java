package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import static org.assertj.core.api.Assertions.assertThat;

import java.io.ByteArrayOutputStream;
import java.util.Arrays;
import java.util.List;

import org.junit.jupiter.api.Test;

import com.alibaba.excel.EasyExcel;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.DatasetTask;
import com.kuaishou.agent.operation.backend.batcheval.service.impl.XlsxDatasetParser.ParseResult;

class XlsxDatasetParserTest {

    @Test
    void shouldParseJsonAlignedColumns() {
        byte[] xlsx = buildWorkbook(
                Arrays.asList("id", "prompt", "linkUrl", "referImages", "extraFields",
                        "category", "conversationKey", "turnIndex"),
                Arrays.asList("001", "第一轮：先出脚本", "https://shop/item",
                        "https://img/a.jpg\nhttps://img/b.jpg",
                        "{\"一级能力域\":\"输入理解\",\"二级能力模块\":\"多源输入\"}",
                        "电商广告", "case-a", "1"),
                Arrays.asList("002", "第二轮：按脚本出片", "", "", "", "", "case-a", "2"));

        ParseResult result = XlsxDatasetParser.parse(xlsx, null);
        DatasetTask first = result.getTasks().get(0);
        assertThat(first.getTaskKey()).isEqualTo("001");
        assertThat(first.getPrompt()).isEqualTo("第一轮：先出脚本");
        assertThat(first.getLinkUrl()).isEqualTo("https://shop/item");
        assertThat(first.getReferImages()).containsExactly("https://img/a.jpg", "https://img/b.jpg");
        assertThat(first.getExtraFields())
                .containsEntry("一级能力域", "输入理解")
                .containsEntry("二级能力模块", "多源输入")
                .doesNotContainKey("prompt");
        assertThat(first.getCategory()).isEqualTo("ecommerce_ad");
        assertThat(first.getConversationKey()).isEqualTo("case-a");
        assertThat(first.getTurnIndex()).isEqualTo(1);
        assertThat(result.getTasks().get(1).getPrompt()).isEqualTo("第二轮：按脚本出片");
        assertThat(result.getTasks().get(1).getTurnIndex()).isEqualTo(2);
        assertThat(result.getColumnMapping()).containsKeys(
                "id", "prompt", "linkUrl", "referImages", "extraFields",
                "category", "conversationKey", "turnIndex");
    }

    @Test
    void shouldParsePromptAndPassthroughExtraColumns() {
        byte[] xlsx = buildWorkbook(
                Arrays.asList("id", "prompt", "一级能力域", "二级", "三级子能力", "能力评估说明"),
                Arrays.asList("001", "帮我生成一张春天草原的图片", "创作迭代", "多模态生成", "图像生成", "生图能力覆盖"),
                Arrays.asList("034", "关注团队资产共享", "团队模式", "团队沉淀", "", "团队资产同步"));

        ParseResult result = XlsxDatasetParser.parse(xlsx, null);
        assertThat(result.getTasks()).hasSize(2);
        DatasetTask first = result.getTasks().get(0);
        assertThat(first.getTaskKey()).isEqualTo("001");
        assertThat(first.getPrompt()).isEqualTo("帮我生成一张春天草原的图片");
        assertThat(first.getExtraFields())
                .containsEntry("一级能力域", "创作迭代")
                .containsEntry("二级", "多模态生成")
                .containsEntry("三级子能力", "图像生成")
                .containsEntry("能力评估说明", "生图能力覆盖")
                .doesNotContainKey("id")
                .doesNotContainKey("prompt");
        assertThat(result.getTasks().get(1).getExtraFields()).containsEntry("三级子能力", "");
    }

    @Test
    void shouldParseLegacyProductAndConversationAliases() {
        byte[] xlsx = buildWorkbook(
                Arrays.asList("序号", "链接", "商品标题", "首图url", "商品价格", "商品详情图", "商品属性信息",
                        "会话ID", "轮次", "本轮指令"),
                Arrays.asList("1", "https://shop/item", "YSL 唇釉", "https://img/cover.jpg", "135.4",
                        "https://img/d1.jpg,https://img/d2.jpg", "色号 N16", "conv-a", "1", "先改图"),
                Arrays.asList("2", "https://shop/item", "YSL 唇釉", "https://img/cover.jpg", "135.4",
                        "", "色号 N16", "conv-a", "2", "再出视频"));

        ParseResult result = XlsxDatasetParser.parse(xlsx, "全局指令");
        DatasetTask first = result.getTasks().get(0);
        assertThat(first.getTaskKey()).isEqualTo("1");
        assertThat(first.getLinkUrl()).isEqualTo("https://shop/item");
        assertThat(first.getReferImages()).containsExactly(
                "https://img/cover.jpg", "https://img/d1.jpg", "https://img/d2.jpg");
        assertThat(first.getExtraFields()).containsEntry("商品标题", "YSL 唇釉");
        assertThat(first.getConversationKey()).isEqualTo("conv-a");
        assertThat(first.getTurnIndex()).isEqualTo(1);
        assertThat(first.getPrompt()).isEqualTo("先改图");
        assertThat(result.getTasks().get(1).getPrompt()).isEqualTo("再出视频");
        assertThat(result.getColumnMapping()).containsKeys(
                "id", "linkUrl", "prompt", "conversationKey", "turnIndex");
    }

    @Test
    void shouldParseProductRowsWithCoverAndDetailImages() {
        byte[] xlsx = buildWorkbook(
                Arrays.asList("序号", "链接", "商品标题", "首图url", "商品价格", "商品详情图", "商品属性信息"),
                Arrays.asList("1", "https://shop/item", "YSL 唇釉", "https://img/cover.jpg", "135.4",
                        "https://img/d1.jpg,https://img/d2.jpg", "色号 N16"),
                Arrays.asList("2", "https://shop/item2", "另一商品", "https://img/c2.jpg", "99",
                        "", "规格 6ml"));

        ParseResult result = XlsxDatasetParser.parse(xlsx, "要求:时长 10 秒,分辨率 1080p");
        assertThat(result.getColumnMapping()).containsKey("商品标题");
        assertThat(result.getTasks()).hasSize(2);

        DatasetTask first = result.getTasks().get(0);
        assertThat(first.getTaskKey()).isEqualTo("1");
        assertThat(first.getLinkUrl()).isEqualTo("https://shop/item");
        assertThat(first.getReferImages()).containsExactly(
                "https://img/cover.jpg", "https://img/d1.jpg", "https://img/d2.jpg");
        assertThat(first.getExtraFields()).containsEntry("商品标题", "YSL 唇釉");
        assertThat(first.getExtraFields()).containsEntry("商品价格", "135.4");
        assertThat(first.getExtraFields()).doesNotContainKey("链接");
        assertThat(first.getPrompt())
                .startsWith("商品标题:YSL 唇釉")
                .contains("商品价格:135.4")
                .endsWith("要求:时长 10 秒,分辨率 1080p")
                .doesNotContain("请根据以下商品信息生成视频广告");
    }

    @Test
    void shouldRejectBlankTaskInstructionWhenPromptMissing() {
        byte[] xlsx = buildWorkbook(
                Arrays.asList("序号", "链接", "商品标题", "首图url", "商品价格", "商品详情图", "商品属性信息"),
                Arrays.asList("1", "https://shop/item", "YSL 唇釉", "https://img/cover.jpg", "135.4",
                        "", "色号 N16"));
        org.assertj.core.api.Assertions.assertThatThrownBy(() -> XlsxDatasetParser.parse(xlsx, "  "))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("taskInstruction 不能为空");
    }

    @Test
    void shouldRoundTripJsonAlignedExport() {
        byte[] uploaded = buildWorkbook(
                Arrays.asList("id", "prompt", "linkUrl", "referImages", "extraFields",
                        "category", "conversationKey", "turnIndex"),
                Arrays.asList("t1", "先出脚本", "https://shop/item", "https://img/a.jpg",
                        "{\"scene\":\"indoor\"}", "ecommerce_ad", "c1", "1"));
        ParseResult parsed = XlsxDatasetParser.parse(uploaded, null);
        byte[] exported = XlsxDatasetWriter.write(parsed.getTasks());
        ParseResult again = XlsxDatasetParser.parse(exported, null);
        DatasetTask task = again.getTasks().get(0);
        assertThat(task.getTaskKey()).isEqualTo("t1");
        assertThat(task.getPrompt()).isEqualTo("先出脚本");
        assertThat(task.getLinkUrl()).isEqualTo("https://shop/item");
        assertThat(task.getReferImages()).containsExactly("https://img/a.jpg");
        assertThat(task.getExtraFields()).containsEntry("scene", "indoor");
        assertThat(task.getCategory()).isEqualTo("ecommerce_ad");
        assertThat(task.getConversationKey()).isEqualTo("c1");
        assertThat(task.getTurnIndex()).isEqualTo(1);
    }

    @Test
    void shouldSplitUrlsByMultipleDelimiters() {
        List<String> urls = XlsxDatasetParser.splitUrls(
                "https://a.jpg\nhttps://b.jpg，https://c.jpg;https://d.jpg");
        assertThat(urls).containsExactly(
                "https://a.jpg", "https://b.jpg", "https://c.jpg", "https://d.jpg");
    }

    private static byte[] buildWorkbook(List<String> head, List<String>... rows) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        EasyExcel.write(out).sheet("sheet1").doWrite(buildData(head, rows));
        return out.toByteArray();
    }

    @SafeVarargs
    private static List<List<String>> buildData(List<String> head, List<String>... rows) {
        List<List<String>> data = new java.util.ArrayList<>();
        data.add(head);
        data.addAll(Arrays.asList(rows));
        return data;
    }
}
