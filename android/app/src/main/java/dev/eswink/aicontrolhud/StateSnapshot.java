package dev.eswink.aicontrolhud;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

final class StateSnapshot {
    static final int SUPPORTED_SCHEMA = 1;

    final String overallStatus;
    final String serverTime;
    final String zcodeHealth;
    final String zcodeObservedAt;
    final String zcodeMessage;
    final Integer running;
    final Integer waiting;
    final Integer failed;
    final Integer completed;
    final List<TaskItem> tasks;
    final String commandHealth;
    final String commandObservedAt;
    final String commandMessage;
    final String plan;
    final Double creditRemaining;
    final Double creditLimit;
    final String creditUnit;
    final UsageWindow fiveHour;
    final UsageWindow weekly;

    private StateSnapshot(
            String overallStatus,
            String serverTime,
            String zcodeHealth,
            String zcodeObservedAt,
            String zcodeMessage,
            Integer running,
            Integer waiting,
            Integer failed,
            Integer completed,
            List<TaskItem> tasks,
            String commandHealth,
            String commandObservedAt,
            String commandMessage,
            String plan,
            Double creditRemaining,
            Double creditLimit,
            String creditUnit,
            UsageWindow fiveHour,
            UsageWindow weekly
    ) {
        this.overallStatus = overallStatus;
        this.serverTime = serverTime;
        this.zcodeHealth = zcodeHealth;
        this.zcodeObservedAt = zcodeObservedAt;
        this.zcodeMessage = zcodeMessage;
        this.running = running;
        this.waiting = waiting;
        this.failed = failed;
        this.completed = completed;
        this.tasks = tasks;
        this.commandHealth = commandHealth;
        this.commandObservedAt = commandObservedAt;
        this.commandMessage = commandMessage;
        this.plan = plan;
        this.creditRemaining = creditRemaining;
        this.creditLimit = creditLimit;
        this.creditUnit = creditUnit;
        this.fiveHour = fiveHour;
        this.weekly = weekly;
    }

    static StateSnapshot parse(String json) throws JSONException, IncompatibleSchemaException {
        JSONObject root = new JSONObject(json);
        int schema = root.getInt("schemaVersion");
        if (schema != SUPPORTED_SCHEMA) throw new IncompatibleSchemaException(schema);

        JSONObject zcode = root.getJSONObject("zcode");
        JSONObject zcodeHealthObject = zcode.getJSONObject("health");
        String zcodeHealth = zcodeHealthObject.getString("status");
        JSONObject summary = zcode.optJSONObject("summary");
        JSONArray tasksJson = zcode.optJSONArray("tasks");
        Integer running = nullableInt(summary, "running");
        Integer waiting = nullableInt(summary, "waiting");
        Integer failed = nullableInt(summary, "failed");
        Integer completed = nullableInt(summary, "completed");

        List<TaskItem> tasks = null;
        if (tasksJson != null) {
            ArrayList<TaskItem> parsedTasks = new ArrayList<>();
            for (int i = 0; i < tasksJson.length(); i++) {
                parsedTasks.add(TaskItem.parse(tasksJson.getJSONObject(i)));
            }
            tasks = Collections.unmodifiableList(parsedTasks);
        }

        JSONObject command = root.getJSONObject("commandCode");
        JSONObject commandHealthObject = command.getJSONObject("health");
        String commandHealth = commandHealthObject.getString("status");
        JSONObject usage = command.optJSONObject("usage");
        String plan = null;
        Double remaining = null;
        Double limit = null;
        String unit = null;
        UsageWindow fiveHour = null;
        UsageWindow weekly = null;
        if (usage != null) {
            plan = nullableString(usage, "plan");
            JSONObject credit = usage.optJSONObject("credit");
            remaining = nullableDouble(credit, "remaining");
            limit = nullableDouble(credit, "limit");
            unit = nullableString(credit, "unit");
            JSONArray windows = usage.optJSONArray("windows");
            if (windows != null) {
                for (int i = 0; i < windows.length(); i++) {
                    UsageWindow parsed = UsageWindow.parse(windows.getJSONObject(i));
                    if ("5h".equals(parsed.name)) fiveHour = parsed;
                    else if ("weekly".equals(parsed.name)) weekly = parsed;
                }
            }
        }

        JSONObject server = root.getJSONObject("server");
        return new StateSnapshot(
                root.getJSONObject("overall").getString("status"),
                nullableString(server, "time"),
                zcodeHealth,
                nullableString(zcodeHealthObject, "observedAt"),
                nullableString(zcodeHealthObject, "message"),
                running,
                waiting,
                failed,
                completed,
                tasks,
                commandHealth,
                nullableString(commandHealthObject, "observedAt"),
                nullableString(commandHealthObject, "message"),
                plan,
                remaining,
                limit,
                unit,
                fiveHour,
                weekly
        );
    }

    private static String nullableString(JSONObject object, String key) {
        if (object == null || object.isNull(key)) return null;
        String value = object.optString(key, null);
        return value == null || value.isEmpty() ? null : value;
    }

    private static Integer nullableInt(JSONObject object, String key) {
        if (object == null || object.isNull(key)) return null;
        return object.optInt(key);
    }

    private static Double nullableDouble(JSONObject object, String key) {
        if (object == null || object.isNull(key)) return null;
        return object.optDouble(key);
    }

    static final class TaskItem {
        final String id;
        final String status;
        final String title;
        final String workspace;
        final String activity;
        final Integer durationSeconds;
        final Integer additions;
        final Integer deletions;

        private TaskItem(
                String id,
                String status,
                String title,
                String workspace,
                String activity,
                Integer durationSeconds,
                Integer additions,
                Integer deletions
        ) {
            this.id = id;
            this.status = status;
            this.title = title;
            this.workspace = workspace;
            this.activity = activity;
            this.durationSeconds = durationSeconds;
            this.additions = additions;
            this.deletions = deletions;
        }

        static TaskItem parse(JSONObject object) throws JSONException {
            JSONObject changes = object.optJSONObject("changes");
            return new TaskItem(
                    object.getString("id"),
                    object.getString("status"),
                    object.getString("title"),
                    nullableString(object, "workspace"),
                    nullableString(object, "activity"),
                    nullableInt(object, "durationSeconds"),
                    nullableInt(changes, "additions"),
                    nullableInt(changes, "deletions")
            );
        }
    }

    static final class UsageWindow {
        final String name;
        final Double usedPercent;
        final String resetAt;

        private UsageWindow(String name, Double usedPercent, String resetAt) {
            this.name = name;
            this.usedPercent = usedPercent;
            this.resetAt = resetAt;
        }

        static UsageWindow parse(JSONObject object) throws JSONException {
            return new UsageWindow(
                    object.getString("name"),
                    nullableDouble(object, "usedPercent"),
                    nullableString(object, "resetAt")
            );
        }
    }

    static final class IncompatibleSchemaException extends Exception {
        final int receivedSchema;

        IncompatibleSchemaException(int receivedSchema) {
            super("Unsupported schemaVersion " + receivedSchema);
            this.receivedSchema = receivedSchema;
        }
    }
}
