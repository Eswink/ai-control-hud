package dev.eswink.aicontrolhud;

import java.util.Calendar;
import java.util.Locale;
import java.util.TimeZone;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

final class IsoTime {
    private static final Pattern ISO_TIMESTAMP = Pattern.compile(
            "^(\\d{4})-(\\d{2})-(\\d{2})T(\\d{2}):(\\d{2}):(\\d{2})(?:\\.\\d+)?(Z|[+-]\\d{2}:?\\d{2})$"
    );

    private IsoTime() {
    }

    static Long parseMillis(String value) {
        if (value == null) return null;
        Matcher match = ISO_TIMESTAMP.matcher(value);
        if (!match.matches()) return null;
        try {
            int year = Integer.parseInt(match.group(1));
            int month = Integer.parseInt(match.group(2));
            int day = Integer.parseInt(match.group(3));
            int hour = Integer.parseInt(match.group(4));
            int minute = Integer.parseInt(match.group(5));
            int second = Integer.parseInt(match.group(6));
            String zone = match.group(7);

            int offsetMinutes = 0;
            if (!"Z".equals(zone)) {
                int sign = zone.charAt(0) == '-' ? -1 : 1;
                int offsetHours = Integer.parseInt(zone.substring(1, 3));
                int offsetMins = Integer.parseInt(zone.substring(zone.length() - 2));
                offsetMinutes = sign * (offsetHours * 60 + offsetMins);
            }

            Calendar calendar = Calendar.getInstance(TimeZone.getTimeZone("UTC"), Locale.US);
            calendar.clear();
            calendar.setLenient(false);
            calendar.set(year, month - 1, day, hour, minute, second);
            return calendar.getTimeInMillis() - offsetMinutes * 60_000L;
        } catch (RuntimeException invalidTimestamp) {
            return null;
        }
    }
}
