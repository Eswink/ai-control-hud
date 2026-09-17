package dev.eswink.aicontrolhud;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

import java.io.IOException;
import java.net.HttpURLConnection;

public final class StateClientTest {
    @Test
    public void state503MeansHubOnlineWaitingForAgent() {
        StateClient.HttpStatusException error = StateClient.httpFailure(
                "/api/v1/state",
                HttpURLConnection.HTTP_UNAVAILABLE
        );

        assertTrue(error instanceof StateClient.SnapshotUnavailableException);
        assertEquals(HttpURLConnection.HTTP_UNAVAILABLE, error.statusCode);
        assertFalse(StateClient.shouldRediscoverAfter(error));
    }

    @Test
    public void otherHttpFailuresDoNotTriggerLanRediscovery() {
        StateClient.HttpStatusException serverError = StateClient.httpFailure(
                "/api/v1/state",
                HttpURLConnection.HTTP_INTERNAL_ERROR
        );
        StateClient.HttpStatusException eventError = StateClient.httpFailure(
                "/api/v1/events?after=0&limit=1",
                HttpURLConnection.HTTP_UNAVAILABLE
        );

        assertFalse(serverError instanceof StateClient.SnapshotUnavailableException);
        assertFalse(eventError instanceof StateClient.SnapshotUnavailableException);
        assertFalse(StateClient.shouldRediscoverAfter(serverError));
        assertFalse(StateClient.shouldRediscoverAfter(eventError));
    }

    @Test
    public void transportFailuresStillTriggerLanRediscovery() {
        assertTrue(StateClient.shouldRediscoverAfter(new IOException("network unreachable")));
    }
}
