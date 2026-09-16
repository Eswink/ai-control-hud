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
    private static final int CONNECT_TIMEOUT_MS = 2500;
    private static final int READ_TIMEOUT_MS = 2500;

    StateSnapshot fetchState(String baseUrl) throws Exception {
        HttpURLConnection connection = open(baseUrl + "/api/v1/state");
        try {
            int code = connection.getResponseCode();
            if (code != HttpURLConnection.HTTP_OK) throw new IOException("state HTTP " + code);
            return StateSnapshot.parse(readAll(connection.getInputStream()));
        } finally {
            connection.disconnect();
        }
    }

    EventPage fetchEvents(String baseUrl, long after, int limit) throws Exception {
        if (after < 0) throw new IllegalArgumentException("event cursor must be non-negative");
        if (limit < 1 || limit > 100) throw new IllegalArgumentException("event page limit must be 1..100");

        HttpURLConnection connection = open(
                baseUrl + "/api/v1/events?after=" + after + "&limit=" + limit
        );
        try {
            int code = connection.getResponseCode();
            if (code != HttpURLConnection.HTTP_OK) throw new IOException("events HTTP " + code);
            EventPage page = EventPage.parse(new JSONObject(readAll(connection.getInputStream())));
            return page.validateForRequest(after);
        } finally {
            connection.disconnect();
        }
    }

    void checkHealth(String baseUrl) throws Exception {
        HttpURLConnection connection = open(baseUrl + "/api/v1/health");
        try {
            int code = connection.getResponseCode();
            if (code != HttpURLConnection.HTTP_OK) throw new IOException("health HTTP " + code);
            JSONObject body = new JSONObject(readAll(connection.getInputStream()));
            int schema = body.getInt("schemaVersion");
            if (schema != StateSnapshot.SUPPORTED_SCHEMA) {
                throw new StateSnapshot.IncompatibleSchemaException(schema);
            }
        } finally {
            connection.disconnect();
        }
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
