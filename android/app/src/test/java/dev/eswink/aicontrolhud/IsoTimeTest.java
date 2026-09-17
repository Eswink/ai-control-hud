package dev.eswink.aicontrolhud;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import org.junit.Test;

public final class IsoTimeTest {
    @Test
    public void parsesZuluAndOffsetAsSameInstant() {
        Long zulu = IsoTime.parseMillis("2026-09-16T12:30:45Z");
        Long offset = IsoTime.parseMillis("2026-09-16T20:30:45+08:00");
        Long compactOffset = IsoTime.parseMillis("2026-09-16T20:30:45+0800");

        assertEquals(zulu, offset);
        assertEquals(zulu, compactOffset);
    }

    @Test
    public void acceptsFractionalSecondsWithoutBreakingLegacyDevices() {
        assertEquals(
                IsoTime.parseMillis("2026-09-16T12:30:45Z"),
                IsoTime.parseMillis("2026-09-16T12:30:45.987654Z")
        );
    }

    @Test
    public void rejectsMalformedAndImpossibleTimestamps() {
        assertNull(IsoTime.parseMillis(null));
        assertNull(IsoTime.parseMillis("not-a-time"));
        assertNull(IsoTime.parseMillis("2026-02-31T12:30:45Z"));
    }
}
