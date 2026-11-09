package com.kuaishou.agent.operation.backend.batcheval.support;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

class DatasetTaskCategoriesTest {

    @Test
    void shouldKeepBlankAsNull() {
        assertThat(DatasetTaskCategories.normalize(null)).isNull();
        assertThat(DatasetTaskCategories.normalize("")).isNull();
        assertThat(DatasetTaskCategories.normalize("   ")).isNull();
    }

    @Test
    void shouldNormalizeEnglishCodesCaseInsensitively() {
        assertThat(DatasetTaskCategories.normalize("product_image"))
                .isEqualTo(DatasetTaskCategories.PRODUCT_IMAGE);
        assertThat(DatasetTaskCategories.normalize("SHORT_DRAMA"))
                .isEqualTo(DatasetTaskCategories.SHORT_DRAMA);
    }

    @Test
    void shouldMapChineseDisplayNames() {
        assertThat(DatasetTaskCategories.normalize("商品图"))
                .isEqualTo(DatasetTaskCategories.PRODUCT_IMAGE);
        assertThat(DatasetTaskCategories.normalize("短剧/漫剧"))
                .isEqualTo(DatasetTaskCategories.SHORT_DRAMA);
        assertThat(DatasetTaskCategories.normalize("走秀/试穿"))
                .isEqualTo(DatasetTaskCategories.RUNWAY);
        assertThat(DatasetTaskCategories.normalize("风格参考图"))
                .isEqualTo(DatasetTaskCategories.STYLE_REF_IMAGE);
    }

    @Test
    void shouldPersistUnknownCategoryAsIs() {
        assertThat(DatasetTaskCategories.normalize("文档解析")).isEqualTo("文档解析");
        assertThat(DatasetTaskCategories.normalize("source_video")).isEqualTo("source_video");
        assertThat(DatasetTaskCategories.normalize("smoke")).isEqualTo("smoke");
    }
}
