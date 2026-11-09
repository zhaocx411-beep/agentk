package com.kuaishou.agent.operation.backend.keffects.ecommerce;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

class EcommerceOutfitReplicationVoicePackTest {

    @Test
    void listOptionsReturnsDisplayNameAndVoiceId() {
        assertThat(EcommerceOutfitReplicationVoicePack.options()).hasSize(10);
        assertThat(EcommerceOutfitReplicationVoicePack.ENERGETIC_SALES_GIRL.toView().getDisplayName())
                .isEqualTo("活力带货女");
        assertThat(EcommerceOutfitReplicationVoicePack.ENERGETIC_SALES_GIRL.toView().getVoiceId())
                .isEqualTo("energetic_sales_girl_260526_f20150");
        assertThat(EcommerceOutfitReplicationVoicePack.options().get(0).getDisplayName()).isNotBlank();
        assertThat(EcommerceOutfitReplicationVoicePack.options().get(0).getVoiceId()).isNotBlank();
    }
}
