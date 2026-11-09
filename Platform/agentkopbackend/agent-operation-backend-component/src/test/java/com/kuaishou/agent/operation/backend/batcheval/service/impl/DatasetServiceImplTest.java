package com.kuaishou.agent.operation.backend.batcheval.service.impl;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

import org.junit.jupiter.api.Test;

import com.alibaba.excel.EasyExcel;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.CreateDatasetTaskRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetExportResult;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetSummaryView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetTaskView;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.DatasetUploadRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.UpdateDatasetRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.BatchEvalDtos.UpdateDatasetTaskRequest;
import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetListFilter;
import com.kuaishou.agent.operation.backend.batcheval.dto.DatasetTaskListFilter;
import com.kuaishou.agent.operation.backend.batcheval.mapper.memory.InMemoryBatchEvalMapper;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJob;
import com.kuaishou.agent.operation.backend.batcheval.model.BatchEvalModels.OfflineJobCell;
import com.kuaishou.agent.operation.backend.batcheval.support.DatasetUsages;

class DatasetServiceImplTest {

    private final InMemoryBatchEvalMapper mapper = new InMemoryBatchEvalMapper();
    private final DatasetServiceImpl service = new DatasetServiceImpl(mapper, new ObjectMapper());

    private DatasetSummaryView upload(byte[] content, String filename, String description, String datasetType,
            String creator) {
        return upload(content, filename, description, datasetType, creator, "demo", null);
    }

    private DatasetSummaryView upload(byte[] content, String filename, String description, String datasetType,
            String creator, String name, String version) {
        return upload(content, filename, description, datasetType, creator, name, version, null,
                DatasetUsages.EVAL, null);
    }

    private DatasetSummaryView upload(byte[] content, String filename, String description, String datasetType,
            String creator, String name, String version, String taskInstruction, String usage, String pipelineName) {
        DatasetUploadRequest request = new DatasetUploadRequest();
        request.setContent(content);
        request.setFilename(filename);
        request.setDescription(description);
        request.setDatasetType(datasetType);
        request.setCreator(creator);
        request.setName(name);
        request.setVersion(version);
        request.setTaskInstruction(taskInstruction);
        request.setUsage(usage);
        request.setPipelineName(pipelineName);
        return service.upload(request);
    }

    @Test
    void shouldUploadReadAndExportMinimalDataset() throws Exception {
        String json = "{\"name\":\"demo\",\"version\":\"v1\",\"tasks\":[{\"id\":\"hello\",\"prompt\":\"请回复 hello\"}]}";

        DatasetSummaryView uploaded = upload(
                json.getBytes(StandardCharsets.UTF_8), "demo.json", "demo dataset", "文本", "tester");

        assertThat(uploaded.getId()).isPositive();
        assertThat(uploaded.getDatasetType()).isEqualTo("文本");
        assertThat(uploaded.getTotal()).isEqualTo(1);
        assertThat(uploaded.getUpdatedAt()).isNotNull();
        assertThat(service.list(new DatasetListFilter(), 1, 10).getTotal()).isEqualTo(1);
        assertThat(service.list(new DatasetListFilter(), 1, 10).getRecords()).hasSize(1);
        assertThat(service.listTasks(uploaded.getId(), 1, 10).getRecords()).hasSize(1);
        assertThat(service.listTasks(uploaded.getId(), 1, 10).getRecords().get(0).getTaskKey())
                .isEqualTo("hello");
        DatasetExportResult exported = service.export(uploaded.getId(), null);
        assertThat(exported.getFormat()).isEqualTo("json");
        com.fasterxml.jackson.databind.JsonNode node = new ObjectMapper().readTree(exported.getBody());
        assertThat(node.path("name").asText()).isEqualTo("demo");
        assertThat(node.path("datasetType").asText()).isEqualTo("文本");
        assertThat(node.path("tasks").get(0).path("id").asText()).isEqualTo("hello");
        assertThat(node.path("tasks").get(0).path("prompt").asText()).contains("hello");
    }

    @Test
    void shouldUploadAndExportJsonDataset() throws Exception {
        String json = "{"
                + "\"name\":\"demo\","
                + "\"version\":\"v1\","
                + "\"tasks\":[{\"id\":\"hello\",\"prompt\":\"请回复 hello\"}]"
                + "}";

        DatasetSummaryView uploaded = upload(
                json.getBytes(StandardCharsets.UTF_8), "demo.json", null, "文本", "tester");

        assertThat(uploaded.getName()).isEqualTo("demo");
        assertThat(service.listTasks(uploaded.getId(), 1, 10).getRecords().get(0).getTaskKey()).isEqualTo("hello");

        ObjectMapper objectMapper = new ObjectMapper();
        DatasetExportResult exportedBytes = service.export(uploaded.getId(), "json");
        assertThat(exportedBytes.getFormat()).isEqualTo("json");
        com.fasterxml.jackson.databind.JsonNode exported =
                objectMapper.readTree(exportedBytes.getBody());
        assertThat(exported.path("name").asText()).isEqualTo("demo");
        assertThat(exported.path("datasetType").asText()).isEqualTo("文本");
        assertThat(exported.path("tasks")).hasSize(1);
        assertThat(exported.path("tasks").get(0).path("id").asText()).isEqualTo("hello");
    }

    @Test
    void shouldDetectJsonByContentWhenFilenameMissing() {
        String json = "{\"name\":\"demo\",\"version\":\"v1\",\"tasks\":[{\"id\":\"a\",\"prompt\":\"p\"}]}";
        DatasetSummaryView uploaded = upload(
                json.getBytes(StandardCharsets.UTF_8), null, null, "图片", "tester");
        assertThat(uploaded.getDatasetType()).isEqualTo("图片");
    }

    @Test
    void shouldAcceptUploadDatasetTypeAndLinkUrl() throws Exception {
        String json = "{"
                + "\"name\":\"demo\","
                + "\"version\":\"v2\","
                + "\"tasks\":[{\"id\":\"browse\",\"prompt\":\"打开链接并总结\",\"linkUrl\":\"https://example.com/page\"}]"
                + "}";

        DatasetSummaryView uploaded = upload(
                json.getBytes(StandardCharsets.UTF_8), "demo.json", null, "视频", "tester");

        assertThat(uploaded.getDatasetType()).isEqualTo("视频");
        DatasetTaskView task = service.listTasks(uploaded.getId(), 1, 10).getRecords().get(0);
        assertThat(task.getLinkUrl()).isEqualTo("https://example.com/page");
        assertThat(task.getReferImages()).containsExactly("https://example.com/page");
        com.fasterxml.jackson.databind.JsonNode exported = new ObjectMapper()
                .readTree(service.export(uploaded.getId(), "json").getBody());
        assertThat(exported.path("datasetType").asText()).isEqualTo("视频");
        assertThat(exported.path("tasks").get(0).path("linkUrl").asText()).isEqualTo("https://example.com/page");
    }

    @Test
    void shouldParseReferImagesAndExtraFieldsFromJson() throws Exception {
        String json = "{"
                + "\"name\":\"demo\","
                + "\"version\":\"v-media\","
                + "\"tasks\":[{"
                + "\"id\":\"t1\","
                + "\"prompt\":\"手写prompt\","
                + "\"referImages\":[\"https://img/a.jpg\",\"https://img/b.jpg\"],"
                + "\"extraFields\":{\"商品标题\":\"YSL\",\"商品价格\":\"135.4\"}"
                + "}]}";
        DatasetSummaryView uploaded = upload(
                json.getBytes(StandardCharsets.UTF_8), "demo.json", null, "视频", "tester");
        DatasetTaskView task = service.listTasks(uploaded.getId(), 1, 10).getRecords().get(0);
        assertThat(task.getReferImages()).containsExactly("https://img/a.jpg", "https://img/b.jpg");
        assertThat(task.getExtraFields()).containsEntry("title", "YSL");
        com.fasterxml.jackson.databind.JsonNode exported = new ObjectMapper()
                .readTree(service.export(uploaded.getId(), "json").getBody());
        assertThat(exported.path("tasks").get(0).path("referImages").get(0).asText()).isEqualTo("https://img/a.jpg");
        assertThat(exported.path("tasks").get(0).path("extraFields").path("商品标题").asText()).isEqualTo("YSL");
        assertThat(exported.path("tasks").get(0).path("extraFields").path("商品价格").asText()).isEqualTo("135.4");
    }

    @Test
    void shouldRejectInvalidExportFormat() {
        DatasetSummaryView uploaded = upload(
                "{\"name\":\"demo\",\"version\":\"v1\",\"tasks\":[{\"id\":\"a\",\"prompt\":\"p\"}]}"
                        .getBytes(StandardCharsets.UTF_8),
                "demo.json", null, "文本", "tester");
        assertThatThrownBy(() -> service.export(uploaded.getId(), "xml"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("format 仅支持 json/xlsx");
        assertThatThrownBy(() -> service.export(uploaded.getId(), "yaml"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("format 仅支持 json/xlsx");
    }

    @Test
    void shouldRejectLinkUrlLongerThanSchemaLimit() {
        char[] chars = new char[1025];
        Arrays.fill(chars, 'a');
        String longUrl = "https://example.com/" + new String(chars);
        String json = "{"
                + "\"name\":\"demo\","
                + "\"version\":\"v3\","
                + "\"tasks\":[{\"id\":\"long-link\",\"prompt\":\"test\",\"linkUrl\":\"" + longUrl + "\"}]"
                + "}";

        assertThatThrownBy(() -> upload(
                json.getBytes(StandardCharsets.UTF_8), "demo.json", null, "文本", "tester"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("linkUrl 长度不能超过 1024");
    }

    @Test
    void shouldUpdateDatasetAndTasks() {
        DatasetSummaryView uploaded = uploadTwoTaskDataset();

        UpdateDatasetRequest updateDataset = new UpdateDatasetRequest();
        updateDataset.setDescription("updated desc");
        updateDataset.setDatasetType("图片");
        DatasetSummaryView updated = service.update(uploaded.getId(), updateDataset);
        assertThat(updated.getDescription()).isEqualTo("updated desc");
        assertThat(updated.getDatasetType()).isEqualTo("图片");

        CreateDatasetTaskRequest createTask = new CreateDatasetTaskRequest();
        createTask.setTaskKey("new-task");
        createTask.setPrompt("new prompt");
        assertThat(service.createTask(uploaded.getId(), createTask).getTaskKey()).isEqualTo("new-task");
        assertThat(service.get(uploaded.getId()).getTotal()).isEqualTo(3);

        long taskId = service.listTasks(uploaded.getId(), 1, 10).getRecords().get(0).getId();
        UpdateDatasetTaskRequest updateTask = new UpdateDatasetTaskRequest();
        updateTask.setPrompt("changed prompt");
        updateTask.setLinkUrl("https://example.com/new");
        updateTask.setReferImages(Collections.singletonList("https://example.com/new"));
        assertThat(service.updateTask(taskId, updateTask).getPrompt()).isEqualTo("changed prompt");

        String exported = new String(service.export(uploaded.getId(), "json").getBody(), StandardCharsets.UTF_8);
        assertThat(exported)
                .contains("updated desc")
                .contains("图片")
                .contains("changed prompt")
                .contains("https://example.com/new")
                .contains("new-task")
                .contains("new prompt");
    }

    @Test
    void shouldExportLatestTasksAfterDelete() {
        DatasetSummaryView uploaded = uploadTwoTaskDataset();
        List<DatasetTaskView> tasks = service.listTasks(uploaded.getId(), 1, 10).getRecords();
        service.deleteTask(tasks.get(0).getId());

        String exported = new String(service.export(uploaded.getId(), "json").getBody(), StandardCharsets.UTF_8);
        assertThat(exported).doesNotContain("\"" + tasks.get(0).getTaskKey() + "\"");
        assertThat(exported).contains("\"" + tasks.get(1).getTaskKey() + "\"");
    }

    @Test
    void shouldExportXlsxWithReferImagesAndExtraFields() {
        DatasetSummaryView uploaded = upload(
                buildMinimalProductXlsx(), "sample.xlsx", "xlsx demo", "图片", "tester",
                "sample", "v1", "请组装上述商品信息，生成10s商品广告视频", DatasetUsages.EVAL, null);

        DatasetTaskView task = service.listTasks(uploaded.getId(), 1, 10).getRecords().get(0);
        UpdateDatasetTaskRequest update = new UpdateDatasetTaskRequest();
        java.util.Map<String, Object> extra = new java.util.LinkedHashMap<>();
        extra.put("商品标题", "改后的标题");
        extra.put("商品价格", "99");
        extra.put("商品属性信息", "色号 N16");
        update.setExtraFields(extra);
        update.setReferImages(Arrays.asList("https://img/new-cover.jpg", "https://img/new-d1.jpg"));
        service.updateTask(task.getId(), update);

        DatasetExportResult exported = service.export(uploaded.getId(), "xlsx");
        assertThat(exported.getFormat()).isEqualTo("xlsx");
        assertThat(exported.getFilename()).endsWith(".xlsx");
        assertThat(exported.getBody()[0]).isEqualTo((byte) 'P');
        assertThat(exported.getBody()[1]).isEqualTo((byte) 'K');

        XlsxDatasetParser.ParseResult reparsed = XlsxDatasetParser.parse(
                exported.getBody(), "请组装上述商品信息，生成10s商品广告视频");
        assertThat(reparsed.getTasks()).hasSize(1);
        assertThat(reparsed.getTasks().get(0).getExtraFields())
                .containsEntry("商品标题", "改后的标题")
                .containsEntry("商品价格", "99");
        assertThat(reparsed.getTasks().get(0).getReferImages())
                .containsExactly("https://img/new-cover.jpg", "https://img/new-d1.jpg");
        assertThat(reparsed.getTasks().get(0).getLinkUrl()).isEqualTo("https://shop/item");
    }

    @Test
    void shouldRejectDeleteDatasetWhenReferencedByJob() {
        DatasetSummaryView uploaded = uploadTwoTaskDataset();
        OfflineJob job = new OfflineJob();
        job.setBizId("job-1");
        job.setDatasetId(uploaded.getId());
        job.setStatus("PENDING");
        job.getCells().add(new OfflineJobCell());
        mapper.insertJob(job);

        assertThat(service.get(uploaded.getId()).isReferencedByJobs()).isTrue();

        assertThatThrownBy(() -> service.delete(uploaded.getId()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("无法删除");
    }

    @Test
    void shouldDeleteDatasetAndCascadeTasksWhenNotReferenced() {
        DatasetSummaryView uploaded = uploadTwoTaskDataset();
        service.delete(uploaded.getId());
        assertThatThrownBy(() -> service.get(uploaded.getId()))
                .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void shouldRejectDeleteLastTask() {
        DatasetSummaryView uploaded = upload(
                "{\"name\":\"solo\",\"version\":\"v1\",\"tasks\":[{\"id\":\"only\",\"prompt\":\"p\"}]}"
                        .getBytes(StandardCharsets.UTF_8),
                "solo.json", null, "文本", "tester");
        long taskId = service.listTasks(uploaded.getId(), 1, 10).getRecords().get(0).getId();
        assertThatThrownBy(() -> service.deleteTask(taskId))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("至少保留 1 条用例");
    }

    @Test
    void shouldFilterDatasetList() {
        upload(
                "{\"name\":\"ignored\",\"version\":\"v1\",\"tasks\":[{\"id\":\"a\",\"prompt\":\"p\"}]}"
                        .getBytes(StandardCharsets.UTF_8),
                "a.json", null, "文本", "alice", "alpha", "v1");
        upload(
                "{\"name\":\"ignored\",\"version\":\"v1\",\"tasks\":[{\"id\":\"b\",\"prompt\":\"p\"}]}"
                        .getBytes(StandardCharsets.UTF_8),
                "b.json", null, "视频", "bob", "beta", "v1");

        DatasetListFilter filter = new DatasetListFilter();
        filter.setName("alpha");
        assertThat(service.list(filter, 1, 10).getTotal()).isEqualTo(1);
        filter.setName(null);
        filter.setDatasetType("视频");
        assertThat(service.list(filter, 1, 10).getRecords().get(0).getName()).isEqualTo("beta");
    }

    @Test
    void shouldRejectUploadWithoutName() {
        assertThatThrownBy(() -> upload(
                "{\"name\":\"file-name\",\"version\":\"v1\",\"tasks\":[{\"id\":\"a\",\"prompt\":\"p\"}]}"
                        .getBytes(StandardCharsets.UTF_8),
                "demo.json", null, "文本", "tester", null, "v1", null, DatasetUsages.EVAL, null))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("name 不能为空");
    }

    @Test
    void shouldIncludeAddedTaskInDatasetTaskList() {
        DatasetSummaryView uploaded = uploadTwoTaskDataset();

        CreateDatasetTaskRequest request = new CreateDatasetTaskRequest();
        request.setTaskKey("added");
        request.setPrompt("new prompt");
        service.createTask(uploaded.getId(), request);

        assertThat(mapper.listAllTasks(uploaded.getId())).hasSize(3);
        assertThat(service.get(uploaded.getId()).getTotal()).isEqualTo(3);
        assertThat(service.listTasks(uploaded.getId(), 1, 10).getTotal()).isEqualTo(3);
    }

    @Test
    void shouldRequireUsageOnUpload() {
        assertThatThrownBy(() -> upload(
                "{\"name\":\"demo\",\"version\":\"v1\",\"tasks\":[{\"id\":\"a\",\"prompt\":\"p\"}]}"
                        .getBytes(StandardCharsets.UTF_8),
                "demo.json", null, "文本", "tester", "demo", "v1", null, null, null))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("usage 不能为空");
    }

    @Test
    void shouldStoreEvalUsageWhenProvided() {
        DatasetSummaryView uploaded = upload(
                "{\"name\":\"demo\",\"version\":\"v1\",\"tasks\":[{\"id\":\"a\",\"prompt\":\"p\"}]}"
                        .getBytes(StandardCharsets.UTF_8),
                "demo.json", null, "文本", "tester");
        assertThat(uploaded.getUsage()).isEqualTo("EVAL");
        assertThat(uploaded.getPipelineName()).isNull();
    }

    @Test
    void shouldRequirePipelineNameForTemplateUsage() {
        assertThatThrownBy(() -> upload(
                "{\"name\":\"tpl\",\"version\":\"v1\",\"tasks\":[{\"id\":\"a\",\"prompt\":\"p\"}]}"
                        .getBytes(StandardCharsets.UTF_8),
                "tpl.json", null, "文本", "tester", "tpl", "v1", null, "TEMPLATE", null))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("pipelineName");
    }

    @Test
    void shouldUploadTemplateJsonWithPipelineName() {
        DatasetUploadRequest request = new DatasetUploadRequest();
        request.setContent(("{\"name\":\"speech-tpl\",\"version\":\"v1\",\"tasks\":["
                + "{\"id\":\"tts-1\",\"prompt\":\"请朗读这段文字\",\"extraFields\":{\"audioUrl\":\"https://cdn/a.mp3\"}}]}")
                .getBytes(StandardCharsets.UTF_8));
        request.setFilename("speech.json");
        request.setDatasetType("文本");
        request.setCreator("admin");
        request.setName("speech-tpl");
        request.setVersion("v1");
        request.setPipelineName("语音合成与识别");

        DatasetSummaryView uploaded = service.uploadTemplate(request);

        assertThat(uploaded.getUsage()).isEqualTo("TEMPLATE");
        assertThat(uploaded.getPipelineName()).isEqualTo("语音合成与识别");
        assertThat(uploaded.getTotal()).isEqualTo(1);
        DatasetTaskView task = service.listTasks(uploaded.getId(), 1, 10).getRecords().get(0);
        assertThat(task.getTaskKey()).isEqualTo("tts-1");
        assertThat(task.getExtraFields()).containsEntry("audioUrl", "https://cdn/a.mp3");
    }

    @Test
    void shouldRejectYamlAndXlsxForTemplateUsage() {
        assertThatThrownBy(() -> upload(
                "name: tpl\nversion: v1\ntasks:\n  - id: a\n    prompt: p\n".getBytes(StandardCharsets.UTF_8),
                "tpl.yaml", null, "文本", "tester", "tpl", "v1", null, "TEMPLATE", "语音合成与识别"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("JSON");

        assertThatThrownBy(() -> upload(
                buildMinimalProductXlsx(), "tpl.xlsx", null, "文本", "tester", "tpl-xlsx", "v1",
                "指令", "TEMPLATE", "语音合成与识别"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("JSON");
    }

    @Test
    void shouldFilterByUsageAndPipelineName() {
        upload(
                "{\"name\":\"eval-ds\",\"version\":\"v1\",\"tasks\":[{\"id\":\"a\",\"prompt\":\"p\"}]}"
                        .getBytes(StandardCharsets.UTF_8),
                "e.json", null, "文本", "tester", "eval-ds", "v1", null, "EVAL", null);
        upload(
                "{\"name\":\"tpl-ds\",\"version\":\"v1\",\"tasks\":[{\"id\":\"b\",\"prompt\":\"p\"}]}"
                        .getBytes(StandardCharsets.UTF_8),
                "t.json", null, "文本", "tester", "tpl-ds", "v1", null, "TEMPLATE", "generate_brand_ad");

        DatasetListFilter evalFilter = new DatasetListFilter();
        evalFilter.setUsage("离线跑数");
        assertThat(service.list(evalFilter, 1, 10).getRecords())
                .extracting(DatasetSummaryView::getName)
                .contains("eval-ds")
                .doesNotContain("tpl-ds");

        DatasetListFilter tplFilter = new DatasetListFilter();
        tplFilter.setUsage("TEMPLATE");
        tplFilter.setPipelineName("generate_brand_ad");
        assertThat(service.list(tplFilter, 1, 10).getRecords())
                .extracting(DatasetSummaryView::getName)
                .containsExactly("tpl-ds");
    }

    @Test
    void shouldListTasksByUsageAndPipelineNameWithoutDatasetId() {
        upload(
                "{\"name\":\"eval-ds\",\"version\":\"v1\",\"tasks\":[{\"id\":\"a\",\"prompt\":\"eval-p\"}]}"
                        .getBytes(StandardCharsets.UTF_8),
                "e.json", null, "文本", "tester", "eval-ds", "v1", null, "EVAL", null);
        DatasetSummaryView tpl = upload(
                ("{\"name\":\"tpl-ds\",\"version\":\"v1\",\"tasks\":["
                        + "{\"id\":\"b\",\"prompt\":\"tpl-p1\"},"
                        + "{\"id\":\"c\",\"prompt\":\"tpl-p2\"}]}")
                        .getBytes(StandardCharsets.UTF_8),
                "t.json", null, "文本", "tester", "tpl-ds", "v1", null, "TEMPLATE", "generate_brand_ad");

        DatasetTaskListFilter filter = new DatasetTaskListFilter();
        filter.setUsage("TEMPLATE");
        filter.setPipelineName("generate_brand_ad");
        assertThat(service.listTasks(filter, 1, 10).getRecords())
                .extracting(DatasetTaskView::getTaskKey)
                .containsExactly("b", "c");
        assertThat(service.listTasks(filter, 1, 10).getRecords())
                .allSatisfy(task -> assertThat(task.getDatasetId()).isEqualTo(tpl.getId()));

        assertThatThrownBy(() -> service.listTasks(new DatasetTaskListFilter(), 1, 10))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("至少传一个");
    }

    @Test
    void shouldCreateUpdateAndFilterTasksByCategory() {
        DatasetSummaryView uploaded = uploadTwoTaskDataset();

        CreateDatasetTaskRequest create = new CreateDatasetTaskRequest();
        create.setTaskKey("goods");
        create.setPrompt("a product");
        create.setReferImages(Collections.singletonList("https://img/p.jpg"));
        create.setCategory("商品图");
        DatasetTaskView created = service.createTask(uploaded.getId(), create);
        assertThat(created.getCategory()).isEqualTo("product_image");

        DatasetTaskListFilter filter = new DatasetTaskListFilter();
        filter.setDatasetId(uploaded.getId());
        filter.setCategory("product_image");
        assertThat(service.listTasks(filter, 1, 10).getRecords())
                .extracting(DatasetTaskView::getTaskKey)
                .containsExactly("goods");

        long firstId = service.listTasks(uploaded.getId(), 1, 10).getRecords().get(0).getId();
        UpdateDatasetTaskRequest update = new UpdateDatasetTaskRequest();
        update.setCategory("short_drama");
        assertThat(service.updateTask(firstId, update).getCategory()).isEqualTo("short_drama");

        UpdateDatasetTaskRequest clear = new UpdateDatasetTaskRequest();
        clear.setCategory("");
        assertThat(service.updateTask(firstId, clear).getCategory()).isNull();

        CreateDatasetTaskRequest custom = new CreateDatasetTaskRequest();
        custom.setTaskKey("doc");
        custom.setPrompt("p");
        custom.setCategory("文档解析");
        assertThat(service.createTask(uploaded.getId(), custom).getCategory()).isEqualTo("文档解析");

        DatasetTaskListFilter customFilter = new DatasetTaskListFilter();
        customFilter.setDatasetId(uploaded.getId());
        customFilter.setCategory("文档解析");
        assertThat(service.listTasks(customFilter, 1, 10).getRecords())
                .extracting(DatasetTaskView::getTaskKey)
                .containsExactly("doc");
    }

    @Test
    void shouldRoundTripCategoryInJsonUploadAndExport() throws Exception {
        String json = "{"
                + "\"name\":\"demo\","
                + "\"version\":\"v-cat\","
                + "\"tasks\":[{\"id\":\"story\",\"prompt\":\"一个短剧梗概\",\"category\":\"short_drama\"},"
                + "{\"id\":\"smoke\",\"prompt\":\"只回复好\",\"category\":\"smoke\"}]"
                + "}";
        DatasetSummaryView uploaded = upload(
                json.getBytes(StandardCharsets.UTF_8), "demo.json", null, "文本", "tester");
        assertThat(service.listTasks(uploaded.getId(), 1, 10).getRecords())
                .extracting(DatasetTaskView::getCategory)
                .containsExactlyInAnyOrder("short_drama", "smoke");
        com.fasterxml.jackson.databind.JsonNode exported = new ObjectMapper()
                .readTree(service.export(uploaded.getId(), "json").getBody());
        assertThat(exported.path("tasks").get(0).path("category").asText()).isIn("short_drama", "smoke");
        assertThat(exported.path("tasks").get(1).path("category").asText()).isIn("short_drama", "smoke");
    }

    @Test
    void shouldRoundTripConversationFieldsInJsonUploadAndExport() throws Exception {
        String json = "{"
                + "\"name\":\"demo\","
                + "\"version\":\"v-conv\","
                + "\"tasks\":["
                + "{\"id\":\"t1\",\"prompt\":\"第一轮\",\"conversation_id\":\"c1\"},"
                + "{\"id\":\"t2\",\"prompt\":\"第二轮\",\"conversationKey\":\"c1\",\"turnIndex\":2},"
                + "{\"id\":\"t3\",\"prompt\":\"单轮\"}"
                + "]}";
        DatasetSummaryView uploaded = upload(
                json.getBytes(StandardCharsets.UTF_8), "demo.json", null, "文本", "tester");
        List<DatasetTaskView> tasks = service.listTasks(uploaded.getId(), 1, 10).getRecords();
        assertThat(tasks).extracting(DatasetTaskView::getTaskKey).containsExactly("t1", "t2", "t3");
        assertThat(tasks.get(0).getConversationKey()).isEqualTo("c1");
        assertThat(tasks.get(0).getTurnIndex()).isEqualTo(1);
        assertThat(tasks.get(1).getConversationKey()).isEqualTo("c1");
        assertThat(tasks.get(1).getTurnIndex()).isEqualTo(2);
        assertThat(tasks.get(2).getConversationKey()).isNull();
        assertThat(tasks.get(2).getTurnIndex()).isNull();

        com.fasterxml.jackson.databind.JsonNode exported = new ObjectMapper()
                .readTree(service.export(uploaded.getId(), "json").getBody());
        assertThat(exported.path("tasks").get(0).path("conversationKey").asText()).isEqualTo("c1");
        assertThat(exported.path("tasks").get(0).path("turnIndex").asInt()).isEqualTo(1);

        assertThatThrownBy(() -> upload(
                ("{\"name\":\"dup\",\"version\":\"v1\",\"tasks\":["
                        + "{\"id\":\"a\",\"prompt\":\"p\",\"conversationKey\":\"c\",\"turnIndex\":1},"
                        + "{\"id\":\"b\",\"prompt\":\"q\",\"conversationKey\":\"c\",\"turnIndex\":1}]}")
                        .getBytes(StandardCharsets.UTF_8),
                "dup.json", null, "文本", "tester", "dup", "v1"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("turnIndex 重复");
    }

    @Test
    void shouldRandomSampleSixTasksForWhitelistedTemplatePipeline() {
        StringBuilder tasksJson = new StringBuilder("[");
        for (int i = 1; i <= 12; i++) {
            if (i > 1) {
                tasksJson.append(',');
            }
            tasksJson.append("{\"id\":\"").append(i).append("\",\"prompt\":\"p").append(i).append("\"}");
        }
        tasksJson.append(']');
        upload(
                ("{\"name\":\"creation-tpl\",\"version\":\"v1\",\"tasks\":" + tasksJson + "}")
                        .getBytes(StandardCharsets.UTF_8),
                "creation.json", null, "文本", "tester", "creation-tpl", "v1", null,
                "TEMPLATE", "完整创作助手");

        DatasetTaskListFilter filter = new DatasetTaskListFilter();
        filter.setUsage("TEMPLATE");
        filter.setPipelineName("完整创作助手");

        assertThat(service.listTasks(filter, 1, 10).getRecords()).hasSize(6);
        assertThat(service.listTasks(filter, 1, 10).getTotal()).isEqualTo(12);
        assertThat(service.listTasks(filter, 2, 10).getRecords()).hasSize(6);
    }

    @Test
    void shouldNotRandomSampleForNonWhitelistedTemplatePipeline() {
        upload(
                ("{\"name\":\"other-tpl\",\"version\":\"v1\",\"tasks\":["
                        + "{\"id\":\"1\",\"prompt\":\"p1\"},"
                        + "{\"id\":\"2\",\"prompt\":\"p2\"},"
                        + "{\"id\":\"3\",\"prompt\":\"p3\"},"
                        + "{\"id\":\"4\",\"prompt\":\"p4\"},"
                        + "{\"id\":\"5\",\"prompt\":\"p5\"},"
                        + "{\"id\":\"6\",\"prompt\":\"p6\"},"
                        + "{\"id\":\"7\",\"prompt\":\"p7\"}"
                        + "]}")
                        .getBytes(StandardCharsets.UTF_8),
                "other.json", null, "文本", "tester", "other-tpl", "v1", null,
                "TEMPLATE", "generate_brand_ad");

        DatasetTaskListFilter filter = new DatasetTaskListFilter();
        filter.setUsage("TEMPLATE");
        filter.setPipelineName("generate_brand_ad");

        assertThat(service.listTasks(filter, 1, 3).getRecords())
                .extracting(DatasetTaskView::getTaskKey)
                .containsExactly("1", "2", "3");
    }

    @Test
    void shouldUploadXlsxEvenWhenFilenameLosesExtension() {
        byte[] xlsx = buildMinimalProductXlsx();
        DatasetSummaryView uploaded = upload(
                xlsx, "测试样本-可上传", null, "视频", "tester", "sample", "v1", "要求:时长 10 秒",
                DatasetUsages.EVAL, null);
        assertThat(uploaded.getTotal()).isEqualTo(1);
        assertThat(uploaded.getName()).isEqualTo("sample");
        DatasetTaskView task = service.listTasks(uploaded.getId(), 1, 10).getRecords().get(0);
        assertThat(task.getLinkUrl()).isEqualTo("https://shop/item");
        assertThat(task.getReferImages()).containsExactly("https://img/cover.jpg", "https://img/d1.jpg");
        assertThat(task.getExtraFields()).doesNotContainKey("链接");
        // /tasks/query：extraFields 投影为 pipeline 入参键，隐藏原始商品列
        assertThat(task.getExtraFields())
                .containsEntry("title", "YSL 唇釉")
                .containsEntry("video_prompt", task.getPrompt())
                .containsEntry("images", task.getReferImages())
                .doesNotContainKeys("商品标题", "商品价格", "商品属性信息");
    }

    private static byte[] buildMinimalProductXlsx() {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        List<List<String>> data = Arrays.asList(
                Arrays.asList("序号", "链接", "商品标题", "首图url", "商品价格", "商品详情图", "商品属性信息"),
                Arrays.asList("1", "https://shop/item", "YSL 唇釉", "https://img/cover.jpg", "135.4",
                        "https://img/d1.jpg", "色号 N16"));
        EasyExcel.write(out).sheet("sheet1").doWrite(data);
        return out.toByteArray();
    }

    private DatasetSummaryView uploadTwoTaskDataset() {
        return upload(
                ("{\"name\":\"demo\",\"version\":\"v-multi\",\"tasks\":["
                        + "{\"id\":\"t1\",\"prompt\":\"p1\"},"
                        + "{\"id\":\"t2\",\"prompt\":\"p2\"}"
                        + "]}").getBytes(StandardCharsets.UTF_8),
                "demo.json", "desc", "文本", "tester");
    }
}
