package dev.eswink.aicontrolhud;

import org.junit.Test;

import static org.junit.Assert.assertEquals;

public final class HubDiscoveryTest {
    @Test
    public void buildsBaseUrlFromResponderAddress() {
        assertEquals(
                "http://192.168.101.103:8787",
                HubDiscovery.buildBaseUrl("http", "192.168.101.103", 8787)
        );
    }

    @Test
    public void autoSentinelHasStableDisplayBeforeDiscovery() {
        assertEquals(
                "AUTO · discovering…",
                StateClient.displayServer(StateClient.AUTO_BASE_URL)
        );
    }

    @Test
    public void manualAddressIsDisplayedUnchanged() {
        assertEquals(
                "http://192.168.101.103:8787",
                StateClient.displayServer("http://192.168.101.103:8787")
        );
    }
}
