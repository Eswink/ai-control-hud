package dev.eswink.aicontrolhud;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import org.junit.Test;

public final class DisplayPolicyTest {
    @Test
    public void keepAwakeRequiresVisibleLandscapeAndEnabledSetting() {
        assertTrue(DisplayPolicy.shouldKeepScreenOn(true, true, true));
        assertFalse(DisplayPolicy.shouldKeepScreenOn(false, true, true));
        assertFalse(DisplayPolicy.shouldKeepScreenOn(true, false, true));
        assertFalse(DisplayPolicy.shouldKeepScreenOn(true, true, false));
    }

    @Test
    public void backgroundAlwaysClearsKeepAwake() {
        for (boolean landscape : new boolean[]{false, true}) {
            for (boolean enabled : new boolean[]{false, true}) {
                assertFalse(DisplayPolicy.shouldKeepScreenOn(false, landscape, enabled));
            }
        }
    }
}
