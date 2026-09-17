package dev.eswink.aicontrolhud;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

final class EventPage {
    static final int SUPPORTED_SCHEMA = 1;

    final int schemaVersion;
    final List<EventItem> events;
    final long nextAfter;
    final long latestSeq;
    final long oldestSeq;

    EventPage(int schemaVersion, List<EventItem> events, long nextAfter, long latestSeq) {
        this(schemaVersion, events, nextAfter, latestSeq, 0L);
    }

    EventPage(int schemaVersion, List<EventItem> events, long nextAfter, long latestSeq, long oldestSeq) {
        this.schemaVersion = schemaVersion;
        this.events = events;
        this.nextAfter = nextAfter;
        this.latestSeq = latestSeq;
        this.oldestSeq = oldestSeq;
    }

    static EventPage parse(JSONObject root) throws JSONException, IncompatibleSchemaException {
        int schemaVersion = root.getInt("schemaVersion");
        if (schemaVersion != SUPPORTED_SCHEMA) throw new IncompatibleSchemaException(schemaVersion);

        long nextAfter = root.getLong("nextAfter");
        long latestSeq = root.getLong("latestSeq");
        long oldestSeq = root.optLong("oldestSeq", 0L);
        if (nextAfter < 0 || latestSeq < 0 || oldestSeq < 0) {
            throw new JSONException("Invalid event cursor metadata");
        }
        if (oldestSeq > 0 && latestSeq > 0 && oldestSeq > latestSeq) {
            throw new JSONException("oldestSeq is ahead of latestSeq");
        }

        JSONArray values = root.getJSONArray("events");
        ArrayList<EventItem> events = new ArrayList<>(values.length());
        for (int i = 0; i < values.length(); i++) {
            JSONObject item = values.getJSONObject(i);
            JSONObject task = item.getJSONObject("task");
            String occurredAt = item.getString("occurredAt");
            Long occurredAtMillis = IsoTime.parseMillis(occurredAt);
            if (occurredAtMillis == null) throw new JSONException("Invalid event occurredAt");

            String type = item.getString("type");
            String status = task.getString("status");
            if (!type.equals("task." + status)) {
                throw new JSONException("Event type/status mismatch");
            }

            events.add(new EventItem(
                    item.getLong("seq"),
                    item.getString("eventId"),
                    item.getString("agentId"),
                    type,
                    occurredAt,
                    occurredAtMillis,
                    item.getString("receivedAt"),
                    task.getString("id"),
                    task.getString("title"),
                    optionalString(task, "workspace"),
                    status
            ));
        }

        return new EventPage(
                schemaVersion,
                Collections.unmodifiableList(events),
                nextAfter,
                latestSeq,
                oldestSeq
        );
    }

    EventPage validateForRequest(long after) throws JSONException {
        if (after < 0) throw new JSONException("Invalid requested event cursor");
        long previous = after;
        for (EventItem event : events) {
            if (event.seq <= previous) throw new JSONException("Non-monotonic event sequence");
            previous = event.seq;
        }
        long expectedNext = events.isEmpty() ? after : previous;
        if (nextAfter != expectedNext) throw new JSONException("Unexpected nextAfter cursor");
        // An empty page with latestSeq < after is valid after a hub database reset.
        // The Android client rebases silently to latestSeq instead of getting stuck.
        if (!events.isEmpty() && latestSeq < nextAfter) {
            throw new JSONException("latestSeq is behind delivered events");
        }
        return this;
    }

    boolean requiresRebase(long after) {
        return events.isEmpty() && latestSeq < after;
    }

    boolean cursorPredatesRetention(long after) {
        return oldestSeq > 0 && after < oldestSeq - 1;
    }

    private static String optionalString(JSONObject object, String key) {
        if (!object.has(key) || object.isNull(key)) return null;
        String value = object.optString(key, null);
        return value == null || value.isEmpty() ? null : value;
    }

    static final class EventItem {
        final long seq;
        final String eventId;
        final String agentId;
        final String type;
        final String occurredAt;
        final long occurredAtMillis;
        final String receivedAt;
        final String taskId;
        final String taskTitle;
        final String workspace;
        final String taskStatus;

        EventItem(
                long seq,
                String eventId,
                String agentId,
                String type,
                String occurredAt,
                long occurredAtMillis,
                String receivedAt,
                String taskId,
                String taskTitle,
                String workspace,
                String taskStatus
        ) {
            this.seq = seq;
            this.eventId = eventId;
            this.agentId = agentId;
            this.type = type;
            this.occurredAt = occurredAt;
            this.occurredAtMillis = occurredAtMillis;
            this.receivedAt = receivedAt;
            this.taskId = taskId;
            this.taskTitle = taskTitle;
            this.workspace = workspace;
            this.taskStatus = taskStatus;
        }
    }

    static final class IncompatibleSchemaException extends Exception {
        final int receivedSchema;

        IncompatibleSchemaException(int receivedSchema) {
            super("Unsupported event schemaVersion " + receivedSchema);
            this.receivedSchema = receivedSchema;
        }
    }
}
