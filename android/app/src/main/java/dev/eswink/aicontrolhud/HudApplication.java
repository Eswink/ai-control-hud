package dev.eswink.aicontrolhud;

import android.app.Application;
import android.content.Context;
import android.content.SharedPreferences;

public final class HudApplication extends Application {
    private static final String PREFS = "hud_settings";
    private static final String KEY_SERVER_URL = "server_url";

    @Override
    public void onCreate() {
        super.onCreate();
        SharedPreferences preferences = getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        if (!preferences.contains(KEY_SERVER_URL)) {
            preferences.edit().putString(KEY_SERVER_URL, StateClient.AUTO_BASE_URL).apply();
        }
    }
}
