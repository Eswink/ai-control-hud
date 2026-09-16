package dev.eswink.aicontrolhud;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

import org.json.JSONObject;
import org.junit.Test;

public final class EventPageTest {
    @Test
    public void emptyPageCanSignalHubCursorReset() throws Exception {
        EventPage page = EventPage.parse(new JSONObject(
                "{\"schemaVersion\":1,\"events\":[],\"nextAfter\":42,\"latestSeq\":3}"
        )).validateForRequest(42L);

        assertTrue(page.requiresRebase(42L));
        assertEquals(3L, page.latestSeq);
    }

    @Test
    public void deliveredEventsRemainStrictlyMonotonic() throws Exception {
        String json = "{"
                + "\"schemaVersion\":1,"
                + "\"events\":[{"
                + "\"seq\":8,"
                + "\"eventId\":\"0123456789abcdef0123456789abcdef\","
                + "\"agentId\":\"desktop-main\","
                + "\"type\":\"task.completed\","
                + "\"occurredAt\":\"2026-09-16T12:00:00Z\","
                + "\"receivedAt\":\"2026-09-16T12:00:01Z\","
                + "\"task\":{\"id\":\"task-1\",\"title\":\"Compile\",\"workspace\":\"backend\",\"status\":\"completed\"}"
                + "}],"
                + "\"nextAfter\":8,"
                + "\"latestSeq\":9"
                + "}";

        EventPage page = EventPage.parse(new JSONObject(json)).validateForRequest(7L);
        assertEquals(1, page.events.size());
        assertEquals(8L, page.nextAfter);
        assertEquals(9L, page.latestSeq);
    }
}
