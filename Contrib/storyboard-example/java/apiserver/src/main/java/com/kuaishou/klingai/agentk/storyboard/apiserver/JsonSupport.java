package com.kuaishou.klingai.agentk.storyboard.apiserver;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.fasterxml.jackson.databind.json.JsonMapper;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;

final class JsonSupport {
    private JsonSupport() {}

    static ObjectMapper createMapper() {
        return JsonMapper.builder()
                .addModule(new JavaTimeModule())
                .defaultPropertyInclusion(
                        JsonInclude.Value.construct(
                                JsonInclude.Include.NON_NULL,
                                JsonInclude.Include.NON_NULL))
                .enable(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES)
                .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS)
                .disable(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS)
                .build();
    }
}
