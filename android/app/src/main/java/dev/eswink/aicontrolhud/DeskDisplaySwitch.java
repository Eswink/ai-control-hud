package dev.eswink.aicontrolhud;

import android.content.Context;
import android.content.SharedPreferences;
import android.content.res.Configuration;
import android.util.AttributeSet;
import android.view.View;
import android.widget.Switch;

public final class DeskDisplaySwitch extends Switch {
    private static final String PREFS = "hud_settings";
    private static final String KEY_DESK_DISPLAY = "desk_display_keep_awake";

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
        setChecked(preferences.getBoolean(KEY_DESK_DISPLAY, false));
        setOnCheckedChangeListener((buttonView, checked) -> {
            preferences.edit().putBoolean(KEY_DESK_DISPLAY, checked).apply();
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
        boolean landscape = getResources().getConfiguration().orientation == Configuration.ORIENTATION_LANDSCAPE;
        setKeepScreenOn(DisplayPolicy.shouldKeepScreenOn(windowVisible, landscape, isChecked()));
    }
}
