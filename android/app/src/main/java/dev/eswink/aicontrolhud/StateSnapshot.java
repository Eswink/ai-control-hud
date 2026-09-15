package dev.eswink.aicontrolhud;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

final class StateSnapshot {
    static final int SUPPORTED_SCHEMA = 1;

    final String overallStatus;
    final String zcodeHealth;
    final Integer running;
    final Integer waiting;
    final Integer failed;
    final Integer completed;
    final String taskStatus;
    final String taskTitle;
    final String taskActivity;
    final String commandHealth;
    final String plan;
    final Double creditRemaining;
    final Double creditLimit;
    final String creditUnit;
    final UsageWindow fiveHour;
    final UsageWindow weekly;

    private StateSnapshot(String overallStatus, String zcodeHealth, Integer running, Integer waiting, Integer failed, Integer completed, String taskStatus, String taskTitle, String taskActivity, String commandHealth, String plan, Double creditRemaining, Double creditLimit, String creditUnit, UsageWindow fiveHour, UsageWindow weekly) {
        this.overallStatus = overallStatus;
        this.zcodeHealth = zcodeHealth;
        this.running = running;
        this.waiting = waiting;
        this.failed = failed;
        this.completed = completed;
        this.taskStatus = taskStatus;
        this.taskTitle = taskTitle;
        this.taskActivity = taskActivity;
        this.commandHealth = commandHealth;
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
        String zcodeHealth = zcode.getJSONObject("health").getString("status");
        JSONObject summary = zcode.optJSONObject("summary");
        JSONArray tasks = zcode.optJSONArray("tasks");
        Integer running = nullableInt(summary, "running");
        Integer waiting = nullableInt(summary, "waiting");
        Integer failed = nullableInt(summary, "failed");
        Integer completed = nullableInt(summary, "completed");

        String taskStatus = null;
        String taskTitle = null;
        String taskActivity = null;
        if (tasks != null && tasks.length() > 0) {
            JSONObject task = tasks.getJSONObject(0);
            taskStatus = nullableString(task, "status");
            taskTitle = nullableString(task, "title");
            taskActivity = nullableString(task, "activity");
        }

        JSONObject command = root.getJSONObject("commandCode");
        String commandHealth = command.getJSONObject("health").getString("status");
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

        return new StateSnapshot(root.getJSONObject("overall").getString("status"), zcodeHealth, running, waiting, failed, completed, taskStatus, taskTitle, taskActivity, commandHealth, plan, remaining, limit, unit, fiveHour, weekly);
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
            return new UsageWindow(object.getString("name"), nullableDouble(object, "usedPercent"), nullableString(object, "resetAt"));
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
