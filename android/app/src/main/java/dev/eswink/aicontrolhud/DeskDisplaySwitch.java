package dev.eswink.aicontrolhud;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.AttributeSet;
import android.view.View;
import android.widget.Switch;

/**
 * User-controlled foreground keep-awake switch.
 *
 * The class and preference key keep their original desk-display names so
 * existing installs preserve the setting across the v0.4.0 upgrade.
 */
public final class DeskDisplaySwitch extends Switch {
    private static final String PREFS = "hud_settings";
    private static final String KEY_KEEP_SCREEN_AWAKE = "desk_display_keep_awake";

    public DeskDisplaySwitch(Context context) {
        super(context);
        initialize();
    }

    public DeskDisplaySwitch(Context context, AttributeSet attrs) {
        super(context, attrs);
        initialize();
    }

    public DeskDisplaySwitch(Context context, AttributeSet attrs, int defStyleAttr) {
        super(context, attrs, defStyleAttr);
        initialize();
    }

    private void initialize() {
        SharedPreferences preferences = getContext().getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        setChecked(preferences.getBoolean(KEY_KEEP_SCREEN_AWAKE, false));
        setOnCheckedChangeListener((buttonView, checked) -> {
            preferences.edit().putBoolean(KEY_KEEP_SCREEN_AWAKE, checked).apply();
            applyDisplayPolicy();
        });
    }

    @Override
    protected void onAttachedToWindow() {
        super.onAttachedToWindow();
        applyDisplayPolicy();
    }

    @Override
    protected void onDetachedFromWindow() {
        setKeepScreenOn(false);
        super.onDetachedFromWindow();
    }

    @Override
    protected void onWindowVisibilityChanged(int visibility) {
        super.onWindowVisibilityChanged(visibility);
        applyDisplayPolicy();
    }

    private void applyDisplayPolicy() {
        boolean windowVisible = isAttachedToWindow() && getWindowVisibility() == View.VISIBLE && isShown();
        setKeepScreenOn(DisplayPolicy.shouldKeepScreenOn(windowVisible, isChecked()));
    }
}
