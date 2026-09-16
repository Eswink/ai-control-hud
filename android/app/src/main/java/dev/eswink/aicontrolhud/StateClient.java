package dev.eswink.aicontrolhud;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;

final class StateClient {
    static final String AUTO_BASE_URL = "http://auto.lan";

    private static final int CONNECT_TIMEOUT_MS = 2500;
    private static final int READ_TIMEOUT_MS = 2500;
    private static final int DISCOVERY_TIMEOUT_MS = 1600;

    private final HubDiscovery discovery = new HubDiscovery();
    private String resolvedAutoBaseUrl;
    private static volatile String lastResolvedAutoBaseUrl;

    StateSnapshot fetchState(String baseUrl) throws Exception {
        return StateSnapshot.parse(get(baseUrl, "/api/v1/state"));
    }

    EventPage fetchEvents(String baseUrl, long after, int limit) throws Exception {
        if (after < 0) throw new IllegalArgumentException("event cursor must be non-negative");
        if (limit < 1 || limit > 100) throw new IllegalArgumentException("event page limit must be 1..100");

        EventPage page = EventPage.parse(new JSONObject(get(
                baseUrl,
                "/api/v1/events?after=" + after + "&limit=" + limit
        )));
        return page.validateForRequest(after);
    }

    void checkHealth(String baseUrl) throws Exception {
        JSONObject body = new JSONObject(get(baseUrl, "/api/v1/health"));
        int schema = body.getInt("schemaVersion");
        if (schema != StateSnapshot.SUPPORTED_SCHEMA) {
            throw new StateSnapshot.IncompatibleSchemaException(schema);
        }
    }

    private String get(String configuredBaseUrl, String path) throws Exception {
        String resolved = resolveBaseUrl(configuredBaseUrl);
        try {
            return getAt(resolved, path);
        } catch (Exception first) {
            if (!AUTO_BASE_URL.equals(configuredBaseUrl)) throw first;
            invalidateAutoResolution(resolved);
            String rediscovered = resolveBaseUrl(configuredBaseUrl);
            if (rediscovered.equals(resolved)) throw first;
            return getAt(rediscovered, path);
        }
    }

    private String getAt(String baseUrl, String path) throws Exception {
        HttpURLConnection connection = open(baseUrl + path);
        try {
            int code = connection.getResponseCode();
            if (code != HttpURLConnection.HTTP_OK) {
                throw new IOException(path + " HTTP " + code);
            }
            return readAll(connection.getInputStream());
        } finally {
            connection.disconnect();
        }
    }

    private synchronized String resolveBaseUrl(String configuredBaseUrl) throws IOException {
        if (!AUTO_BASE_URL.equals(configuredBaseUrl)) return configuredBaseUrl;
        if (resolvedAutoBaseUrl != null && !resolvedAutoBaseUrl.isEmpty()) return resolvedAutoBaseUrl;
        HubDiscovery.Result result = discovery.discover(DISCOVERY_TIMEOUT_MS);
        resolvedAutoBaseUrl = result.baseUrl;
        lastResolvedAutoBaseUrl = result.baseUrl;
        return resolvedAutoBaseUrl;
    }

    private synchronized void invalidateAutoResolution(String expected) {
        if (expected != null && expected.equals(resolvedAutoBaseUrl)) {
            resolvedAutoBaseUrl = null;
        }
    }

    static String displayServer(String configuredText) {
        if (configuredText == null) return "";
        if (!configuredText.startsWith(AUTO_BASE_URL)) return configuredText;
        String suffix = configuredText.substring(AUTO_BASE_URL.length());
        String resolved = lastResolvedAutoBaseUrl;
        if (resolved == null || resolved.isEmpty()) {
            return "AUTO · discovering…" + suffix;
        }
        return "AUTO · " + resolved + suffix;
    }

    private HttpURLConnection open(String address) throws IOException {
        HttpURLConnection connection = (HttpURLConnection) new URL(address).openConnection();
        connection.setRequestMethod("GET");
        connection.setConnectTimeout(CONNECT_TIMEOUT_MS);
        connection.setReadTimeout(READ_TIMEOUT_MS);
        connection.setUseCaches(false);
        connection.setRequestProperty("Accept", "application/json");
        connection.setRequestProperty("Connection", "close");
        return connection;
    }

    private static String readAll(InputStream stream) throws IOException {
        StringBuilder builder = new StringBuilder(4096);
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(stream, StandardCharsets.UTF_8))) {
            char[] buffer = new char[2048];
            int read;
            while ((read = reader.read(buffer)) != -1) {
                builder.append(buffer, 0, read);
                if (builder.length() > 512_000) throw new IOException("response exceeds 512 KiB limit");
            }
        }
        return builder.toString();
    }
}
