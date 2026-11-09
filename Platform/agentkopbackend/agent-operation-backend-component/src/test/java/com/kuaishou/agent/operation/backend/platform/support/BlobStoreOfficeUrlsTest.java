package com.kuaishou.agent.operation.backend.platform.support;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

class BlobStoreOfficeUrlsTest {

    @Test
    void rewritesIdcAnonymousUrl() {
        String idc = "http://bs3-hb1.internal/user-zhangyuchen05/capabilities/images/abc.png";
        assertThat(BlobStoreOfficeUrls.toOfficeUrl(idc, "https://bs3-hb1.corp.kuaishou.com"))
                .isEqualTo("https://bs3-hb1.corp.kuaishou.com/user-zhangyuchen05/capabilities/images/abc.png");
    }

    @Test
    void leavesStagingOfficeUrlAlone() {
        String staging = "https://bs3-hb1.staging.kuaishou.com/user-x/images/a.png";
        assertThat(BlobStoreOfficeUrls.toOfficeUrl(staging, "https://bs3-hb1.corp.kuaishou.com"))
                .isEqualTo(staging);
    }

    @Test
    void rewritesHostInsideMarkdown() {
        String text = "![图](http://bs3-hb1.internal/bucket/key.png)";
        assertThat(BlobStoreOfficeUrls.toOfficeUrl(text, "https://bs3-hb1.corp.kuaishou.com"))
                .isEqualTo("![图](https://bs3-hb1.corp.kuaishou.com/bucket/key.png)");
    }
}
