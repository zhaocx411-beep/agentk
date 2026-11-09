package com.kuaishou.agent.operation.backend.agentk.grpc;

import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.Map;

/**
 * 仅本地把 {@code KWS_SERVICE_NAME} 写进当前进程环境。
 * 必须用 ProcessEnvironment 的 Variable/Value 作为 map key，直接 put String 会把 getenv 弄坏。
 */
final class KwsProcessEnv {

    private KwsProcessEnv() {
    }

    @SuppressWarnings("unchecked")
    static boolean put(String key, String value) {
        try {
            Class<?> processEnv = Class.forName("java.lang.ProcessEnvironment");
            Object variable = envToken(processEnv, "Variable", key);
            Object tokenValue = envToken(processEnv, "Value", value);
            Field envField = processEnv.getDeclaredField("theEnvironment");
            envField.setAccessible(true);
            ((Map<Object, Object>) envField.get(null)).put(variable, tokenValue);
            return value.equals(System.getenv(key));
        } catch (Exception e) {
            return false;
        }
    }

    private static Object envToken(Class<?> processEnv, String nested, String text)
            throws ReflectiveOperationException {
        Class<?> type = Class.forName(processEnv.getName() + "$" + nested);
        Method valueOf = type.getDeclaredMethod("valueOf", String.class);
        valueOf.setAccessible(true);
        return valueOf.invoke(null, text);
    }
}
