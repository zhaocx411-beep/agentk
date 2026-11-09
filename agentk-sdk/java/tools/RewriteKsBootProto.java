import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Comparator;
import java.util.List;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.stream.Collectors;
import java.util.stream.Stream;

/** Creates the proto3-only source view consumed by the KS Boot Java code generator. */
public final class RewriteKsBootProto {
    private static final Pattern EDITION =
            Pattern.compile("(?m)^edition = \"2023\";\\R");
    private static final Pattern IMPLICIT_PRESENCE =
            Pattern.compile("(?m)^option features\\.field_presence = IMPLICIT;\\R");
    private static final Pattern ANY_EDITION =
            Pattern.compile("(?m)^\\s*edition\\s*=");
    private static final Pattern ANY_FEATURE = Pattern.compile("\\bfeatures\\.");

    private RewriteKsBootProto() {}

    public static void main(String[] args) throws Exception {
        if (args.length != 2) {
            throw new IllegalArgumentException("usage: RewriteKsBootProto SOURCE_DIR TARGET_DIR");
        }
        Path source = Path.of(args[0]).toAbsolutePath().normalize();
        Path target = Path.of(args[1]).toAbsolutePath().normalize();
        if (!Files.isDirectory(source)) {
            throw new IllegalArgumentException("KS Boot proto source is not a directory: " + source);
        }
        if (source.equals(target) || source.startsWith(target)) {
            throw new IllegalArgumentException("KS Boot proto target must not contain its source: " + target);
        }

        deleteTree(target);
        List<Path> protos;
        try (Stream<Path> paths = Files.walk(source)) {
            protos = paths.filter(Files::isRegularFile)
                    .filter(path -> path.getFileName().toString().endsWith(".proto"))
                    .sorted()
                    .collect(Collectors.toList());
        }
        if (protos.isEmpty()) {
            throw new IllegalStateException("KS Boot proto source contains no .proto files: " + source);
        }

        for (Path proto : protos) {
            Path relative = source.relativize(proto);
            String original = Files.readString(proto, StandardCharsets.UTF_8);
            requireExactlyOne(EDITION, original, relative, "edition = \"2023\";");
            requireExactlyOne(
                    IMPLICIT_PRESENCE,
                    original,
                    relative,
                    "option features.field_presence = IMPLICIT;");
            requireExactlyOne(
                    ANY_FEATURE,
                    original,
                    relative,
                    "the supported field_presence feature");

            String rewritten = EDITION.matcher(original)
                    .replaceFirst("syntax = \"proto3\";" + System.lineSeparator());
            rewritten = IMPLICIT_PRESENCE.matcher(rewritten).replaceFirst("");
            if (ANY_EDITION.matcher(rewritten).find() || ANY_FEATURE.matcher(rewritten).find()) {
                throw new IllegalStateException(
                        "unsupported Editions syntax remains after rewriting " + relative);
            }

            Path output = target.resolve(relative);
            Files.createDirectories(output.getParent());
            Files.writeString(output, rewritten, StandardCharsets.UTF_8);
        }
        System.out.println("rewrote " + protos.size() + " KS Boot proto files into " + target);
    }

    private static void requireExactlyOne(
            Pattern pattern, String content, Path file, String description) {
        Matcher matcher = pattern.matcher(content);
        int count = 0;
        while (matcher.find()) {
            count++;
        }
        if (count != 1) {
            throw new IllegalStateException(
                    file + " must contain exactly one " + description + "; found " + count);
        }
    }

    private static void deleteTree(Path root) throws IOException {
        if (!Files.exists(root)) {
            return;
        }
        try (Stream<Path> paths = Files.walk(root)) {
            for (Path path : paths.sorted(Comparator.reverseOrder()).collect(Collectors.toList())) {
                Files.delete(path);
            }
        }
    }
}
