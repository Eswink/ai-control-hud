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

    static boolean isTooOld(EventPage.EventItem event, long nowMillis) {
        return event != null && nowMillis - event.occurredAtMillis > MAX_SPEECH_AGE_MS;
    }

    static boolean isQuietMinute(int localMinuteOfDay) {
        int minute = Math.max(0, Math.min(24 * 60 - 1, localMinuteOfDay));
        return minute >= QUIET_START_MINUTE || minute < QUIET_END_MINUTE;
    }

    static String speechText(EventPage.EventItem event, Locale locale) {
        String title = event == null || event.taskTitle == null || event.taskTitle.trim().isEmpty()
                ? "task"
                : event.taskTitle.trim();
        boolean chinese = isChinese(locale);

        if (event != null && "task.failed".equals(event.type)) {
            return chinese ? "任务「" + title + "」执行失败。" : "Task " + title + " failed.";
        }
        return chinese ? "任务「" + title + "」已完成。" : "Task " + title + " completed.";
    }

    static String offlineSummaryText(Locale locale) {
        return isChinese(locale)
                ? "离线期间有较早的任务状态更新，已同步到控制面板。"
                : "Older task updates were received while the HUD was offline.";
    }

    static String testSpeech(Locale locale) {
        return isChinese(locale)
                ? "AI 控制面板语音提醒正常。"
                : "AI Control HUD voice notifications are working.";
    }

    private static boolean isChinese(Locale locale) {
        return locale != null && locale.getLanguage().toLowerCase(Locale.US).startsWith("zh");
    }
}
