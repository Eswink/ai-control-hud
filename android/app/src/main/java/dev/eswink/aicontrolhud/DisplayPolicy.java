package dev.eswink.aicontrolhud;

final class DisplayPolicy {
    private DisplayPolicy() {}

    static boolean shouldKeepScreenOn(boolean windowVisible, boolean landscape, boolean deskDisplayEnabled) {
        return windowVisible && landscape && deskDisplayEnabled;
    }
}
