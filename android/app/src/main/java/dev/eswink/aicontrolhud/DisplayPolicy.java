package dev.eswink.aicontrolhud;

final class DisplayPolicy {
    private DisplayPolicy() {}

    static boolean shouldKeepScreenOn(boolean windowVisible, boolean keepAwakeEnabled) {
        return windowVisible && keepAwakeEnabled;
    }
}
