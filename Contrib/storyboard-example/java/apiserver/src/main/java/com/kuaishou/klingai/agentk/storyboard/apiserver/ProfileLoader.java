package com.kuaishou.klingai.agentk.storyboard.apiserver;

import com.google.protobuf.util.JsonFormat;
import com.kuaishou.klingai.agentk.api.v1.Profile;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

final class ProfileLoader {
    private ProfileLoader() {}

    static Profile load(Path path) throws IOException {
        Profile.Builder builder = Profile.newBuilder();
        try {
            JsonFormat.parser().merge(Files.readString(path), builder);
        } catch (com.google.protobuf.InvalidProtocolBufferException error) {
            throw new IOException("decode profile " + path + ": " + error.getMessage(), error);
        }
        return builder.build();
    }
}
