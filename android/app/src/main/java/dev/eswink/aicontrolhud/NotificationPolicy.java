package dev.eswink.aicontrolhud;

import java.util.Locale;

final class NotificationPolicy {
    static final int QUIET_START_MINUTE = 23 * 60;
    static final int QUIET_END_MINUTE = 8 * 60;
    static final long MAX_SPEECH_AGE_MS = 10 * 60 * 1000L;

    private NotificationPolicy() {
    }

    static boolean shouldSpeak(
            EventPage.EventItem event,
            boolean voiceEnabled,
            boolean quietHoursEnabled,
            int localMinuteOfDay,
            long nowMillis
    ) {
        if (!voiceEnabled || event == null) return false;
        if (!"task.completed".equals(event.type) && !"task.failed".equals(event.type)) return false;
        if (quietHoursEnabled && isQuietMinute(localMinuteOfDay)) return false;

        long age = nowMillis - event.occurredAtMillis;
        if (age > MAX_SPEECH_AGE_MS) return false;
        // Small negative ages are tolerated because task timestamps originate on the
        // development machine and may have minor clock skew relative to the phone.
        return age >= -5 * 60 * 1000L;
    }

    static boolean isQuietMinute(int localMinuteOfDay) {
        int minute = Math.max(0, Math.min(24 * 60 - 1, localMinuteOfDay));
        return minute >= QUIET_START_MINUTE || minute < QUIET_END_MINUTE;
    }

    static String speechText(EventPage.EventItem event, Locale locale) {
        String title = event == null || event.taskTitle == null || event.taskTitle.trim().isEmpty()
                ? "task"
                : event.taskTitle.trim();
        boolean chinese = locale != null && locale.getLanguage().toLowerCase(Locale.US).startsWith("zh");

        if (event != null && "task.failed".equals(event.type)) {
            return chinese ? "任务「" + title + "」执行失败。" : "Task " + title + " failed.";
        }
        return chinese ? "任务「" + title + "」已完成。" : "Task " + title + " completed.";
    }

    static String testSpeech(Locale locale) {
        boolean chinese = locale != null && locale.getLanguage().toLowerCase(Locale.US).startsWith("zh");
        return chinese ? "AI 控制面板语音提醒正常。" : "AI Control HUD voice notifications are working.";
    }
}
