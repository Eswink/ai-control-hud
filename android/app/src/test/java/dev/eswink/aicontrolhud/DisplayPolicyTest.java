package dev.eswink.aicontrolhud;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

public final class DisplayPolicyTest {
    @Test
    public void keepAwakeRequiresVisibleHudAndEnabledSetting() {
        assertTrue(DisplayPolicy.shouldKeepScreenOn(true, true));
        assertFalse(DisplayPolicy.shouldKeepScreenOn(false, true));
        assertFalse(DisplayPolicy.shouldKeepScreenOn(true, false));
    }

    @Test
    public void backgroundAlwaysClearsKeepAwake() {
        for (boolean enabled : new boolean[]{false, true}) {
            assertFalse(DisplayPolicy.shouldKeepScreenOn(false, enabled));
        }
    }
}
