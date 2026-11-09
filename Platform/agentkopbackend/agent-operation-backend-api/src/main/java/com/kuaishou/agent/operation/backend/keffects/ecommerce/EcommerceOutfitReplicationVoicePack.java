package com.kuaishou.agent.operation.backend.keffects.ecommerce;

import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.stream.Collectors;

import com.kuaishou.agent.operation.backend.keffects.dto.EcommerceOutfitReplicationVoiceView;

/**
 * 电商一键复刻（服装）配音包配置，对齐 {@code ecommerceOutfitReplicationVoicePackConfig.json}。
 */
public enum EcommerceOutfitReplicationVoicePack {

    ENERGETIC_SALES_GIRL(
            "energetic_sales_girl",
            "活力带货女",
            "energetic_sales_girl_260526_f20150"),
    GENTLE_SALES_WOMAN(
            "gentle_sales_woman",
            "温柔带货女",
            "gentle_sales_woman_260526_3d0c5a"),
    CRISP_SALES_GIRL(
            "crisp_sales_girl",
            "清脆带货女",
            "crisp_sales_girl_260526_b04fdb"),
    MATURE_SALES_WOMAN(
            "mature_sales_woman",
            "知性带货女",
            "mature_sales_woman_260526_adb1d6"),
    CALM_SALES_WOMAN(
            "calm_sales_woman",
            "平静带货女",
            "calm_sales_woman_260526_65b6c3"),
    SOFT_SALES_WOMAN(
            "soft_sales_woman",
            "柔和带货女",
            "soft_sales_woman_260526_d1f440"),
    ENERGETIC_SALES_BOY(
            "energetic_sales_boy",
            "活力带货男",
            "energetic_sales_boy_260526_fd57a0"),
    ENTHUSIASTIC_SALES_MAN(
            "enthusiastic_sales_man",
            "热情带货男",
            "enthusiastic_sales_man_260526_e04947"),
    STEADY_SALES_MAN(
            "steady_sales_man",
            "沉稳带货男",
            "steady_sales_man_260526_994ad6"),
    CALM_SALES_MAN(
            "calm_sales_man",
            "平静带货男",
            "calm_sales_man_260526_c27eed");

    private final String code;
    private final String displayName;
    private final String voiceId;

    EcommerceOutfitReplicationVoicePack(String code, String displayName, String voiceId) {
        this.code = code;
        this.displayName = displayName;
        this.voiceId = voiceId;
    }

    public String getCode() {
        return code;
    }

    public String getDisplayName() {
        return displayName;
    }

    public String getVoiceId() {
        return voiceId;
    }

    public EcommerceOutfitReplicationVoiceView toView() {
        EcommerceOutfitReplicationVoiceView view = new EcommerceOutfitReplicationVoiceView();
        view.setDisplayName(displayName);
        view.setVoiceId(voiceId);
        return view;
    }

    public static List<EcommerceOutfitReplicationVoiceView> listOptions() {
        return Arrays.stream(values())
                .map(EcommerceOutfitReplicationVoicePack::toView)
                .collect(Collectors.toList());
    }

    public static List<EcommerceOutfitReplicationVoiceView> options() {
        return Collections.unmodifiableList(listOptions());
    }
}
