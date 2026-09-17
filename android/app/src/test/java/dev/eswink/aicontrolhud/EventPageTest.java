package dev.eswink.aicontrolhud;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.json.JSONObject;
import org.junit.Test;

import java.util.Collections;
import java.util.List;

public final class EventPageTest {
    @Test
    public void emptyPageCanSignalHubCursorReset() throws Exception {
        EventPage page = new EventPage(
                EventPage.SUPPORTED_SCHEMA,
                Collections.emptyList(),
                42L,
                3L
        ).validateForRequest(42L);

        assertTrue(page.requiresRebase(42L));
        assertEquals(3L, page.latestSeq);
        assertFalse(page.cursorPredatesRetention(42L));
    }

    @Test
    public void deliveredEventsRemainStrictlyMonotonic() throws Exception {
        EventPage.EventItem event = new EventPage.EventItem(
                8L,
                "0123456789abcdef0123456789abcdef",
                "desktop-main",
                "task.completed",
                "2026-09-16T12:00:00Z",
                1_789_560_000_000L,
                "2026-09-16T12:00:01Z",
                "task-1",
                "Compile",
                "backend",
                "completed"
        );
        EventPage page = new EventPage(
                EventPage.SUPPORTED_SCHEMA,
                List.of(event),
                8L,
                9L
        ).validateForRequest(7L);

        assertEquals(1, page.events.size());
        assertEquals(8L, page.nextAfter);
        assertEquals(9L, page.latestSeq);
    }

    @Test
    public void retentionFloorSignalsIncompleteSavedHistory() throws Exception {
        EventPage page = new EventPage(
                EventPage.SUPPORTED_SCHEMA,
                Collections.emptyList(),
                1000L,
                1500L,
                1001L
        );

        assertTrue(page.cursorPredatesRetention(900L));
        assertFalse(page.cursorPredatesRetention(1000L));
        assertFalse(page.cursorPredatesRetention(1200L));
    }

    @Test
    public void parserAcceptsAdditiveOldestSeqWithoutSchemaBump() throws Exception {
        JSONObject root = new JSONObject("{"
                + "\"schemaVersion\":1,"
                + "\"events\":[],"
                + "\"nextAfter\":900,"
                + "\"latestSeq\":1500,"
                + "\"oldestSeq\":1001"
                + "}");
        EventPage page = EventPage.parse(root).validateForRequest(900L);
        assertEquals(1001L, page.oldestSeq);
        assertTrue(page.cursorPredatesRetention(900L));
    }
}
