package dev.eswink.aicontrolhud;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

import java.util.Locale;

public final class NotificationPolicyTest {
    private static final long NOW = 1_700_000_000_000L;

    @Test
    public void voiceDisabledNeverSpeaks() {
        assertFalse(NotificationPolicy.shouldSpeak(
                event("task.completed", NOW - 1_000L),
                false,
                false,
                12 * 60,
                NOW
        ));
    }

    @Test
    public void quietHoursWrapAcrossMidnight() {
        EventPage.EventItem event = event("task.completed", NOW - 1_000L);

        assertFalse(NotificationPolicy.shouldSpeak(event, true, true, 23 * 60, NOW));
        assertFalse(NotificationPolicy.shouldSpeak(event, true, true, 0, NOW));
        assertFalse(NotificationPolicy.shouldSpeak(event, true, true, 7 * 60 + 59, NOW));
        assertTrue(NotificationPolicy.shouldSpeak(event, true, true, 8 * 60, NOW));
        assertTrue(NotificationPolicy.shouldSpeak(event, true, true, 22 * 60 + 59, NOW));
    }

    @Test
    public void quietHoursCanBeDisabled() {
        assertTrue(NotificationPolicy.shouldSpeak(
                event("task.completed", NOW - 1_000L),
                true,
                false,
                2 * 60,
                NOW
        ));
    }

    @Test
    public void oldOfflineEventsAreConsumedSilently() {
        EventPage.EventItem old = event(
                "task.completed",
                NOW - NotificationPolicy.MAX_SPEECH_AGE_MS - 1L
        );
        assertFalse(NotificationPolicy.shouldSpeak(old, true, false, 12 * 60, NOW));
        assertTrue(NotificationPolicy.isTooOld(old, NOW));

        EventPage.EventItem boundary = event(
                "task.completed",
                NOW - NotificationPolicy.MAX_SPEECH_AGE_MS
        );
        assertTrue(NotificationPolicy.shouldSpeak(boundary, true, false, 12 * 60, NOW));
        assertFalse(NotificationPolicy.isTooOld(boundary, NOW));
    }

    @Test
    public void largeFutureClockSkewIsSuppressed() {
        assertFalse(NotificationPolicy.shouldSpeak(
                event("task.completed", NOW + 5 * 60 * 1000L + 1L),
                true,
                false,
                12 * 60,
                NOW
        ));
    }

    @Test
    public void unsupportedEventTypeDoesNotSpeak() {
        assertFalse(NotificationPolicy.shouldSpeak(
                event("agent.offline", NOW - 1_000L),
                true,
                false,
                12 * 60,
                NOW
        ));
    }

    @Test
    public void speechTextUsesDeviceLanguage() {
        EventPage.EventItem completed = event("task.completed", NOW);
        EventPage.EventItem failed = event("task.failed", NOW);

        assertEquals("Task Compile backend completed.", NotificationPolicy.speechText(completed, Locale.US));
        assertEquals("Task Compile backend failed.", NotificationPolicy.speechText(failed, Locale.US));
        assertEquals("任务「Compile backend」已完成。", NotificationPolicy.speechText(completed, Locale.SIMPLIFIED_CHINESE));
        assertEquals("任务「Compile backend」执行失败。", NotificationPolicy.speechText(failed, Locale.SIMPLIFIED_CHINESE));
    }

    @Test
    public void offlineSummaryUsesDeviceLanguage() {
        assertEquals(
                "Older task updates were received while the HUD was offline.",
                NotificationPolicy.offlineSummaryText(Locale.US)
        );
        assertEquals(
                "离线期间有较早的任务状态更新，已同步到控制面板。",
                NotificationPolicy.offlineSummaryText(Locale.SIMPLIFIED_CHINESE)
        );
    }

    private static EventPage.EventItem event(String type, long occurredAtMillis) {
        String status;
        if ("task.failed".equals(type)) {
            status = "failed";
        } else if ("task.completed".equals(type)) {
            status = "completed";
        } else {
            status = "unknown";
        }
        return new EventPage.EventItem(
                1L,
                "0123456789abcdef0123456789abcdef",
                "desktop-main",
                type,
                "2026-09-16T12:00:00Z",
                occurredAtMillis,
                "2026-09-16T12:00:01Z",
                "task-1",
                "Compile backend",
                "backend",
                status
        );
    }
}
