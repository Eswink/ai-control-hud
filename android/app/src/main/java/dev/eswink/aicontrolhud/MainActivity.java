package dev.eswink.aicontrolhud;

import android.app.Activity;
import android.content.Context;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.View;
import android.view.WindowManager;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.ScrollView;
import android.widget.TextView;

import java.util.Locale;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicBoolean;

public final class MainActivity extends Activity {
    private static final String PREFS = "hud_settings";
    private static final String KEY_SERVER_URL = "server_url";
    private static final long POLL_MS = 2000L;
    private static final long MAX_BACKOFF_MS = 30000L;

    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final ExecutorService networkExecutor = Executors.newSingleThreadExecutor();
    private final AtomicBoolean requestInFlight = new AtomicBoolean(false);
    private final StateClient client = new StateClient();

    private SharedPreferences preferences;
    private boolean polling;
    private int failureCount;
    private String serverUrl;

    private LinearLayout setupPanel;
    private ScrollView dashboardPanel;
    private EditText serverUrlInput;
    private Button connectButton;
    private TextView setupStatus;
    private TextView liveStatus;
    private TextView serverLabel;
    private TextView zcodeHealth;
    private TextView zcodeSummary;
    private TextView taskStatus;
    private TextView taskTitle;
    private TextView taskActivity;
    private TextView commandHealth;
    private TextView planText;
    private TextView creditText;
    private TextView fiveHourLabel;
    private TextView weeklyLabel;
    private ProgressBar fiveHourProgress;
    private ProgressBar weeklyProgress;

    private final Runnable pollRunnable = this::requestState;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        setContentView(R.layout.activity_main);
        bindViews();
        preferences = getSharedPreferences(PREFS, Context.MODE_PRIVATE);

        connectButton.setOnClickListener(view -> testAndSaveServer());
        findViewById(R.id.changeServerButton).setOnClickListener(view -> {
            stopPolling();
            showSetup(serverUrl);
        });
    }

    @Override
    protected void onStart() {
        super.onStart();
        enterImmersiveMode();
        serverUrl = preferences.getString(KEY_SERVER_URL, null);
        if (serverUrl == null || serverUrl.isEmpty()) {
            showSetup("");
        } else {
            showDashboard();
            startPolling();
        }
    }

    @Override
    protected void onStop() {
        stopPolling();
        super.onStop();
    }

    @Override
    protected void onDestroy() {
        networkExecutor.shutdownNow();
        super.onDestroy();
    }

    private void bindViews() {
        setupPanel = findViewById(R.id.setupPanel);
        dashboardPanel = findViewById(R.id.dashboardPanel);
        serverUrlInput = findViewById(R.id.serverUrlInput);
        connectButton = findViewById(R.id.connectButton);
        setupStatus = findViewById(R.id.setupStatus);
        liveStatus = findViewById(R.id.liveStatus);
        serverLabel = findViewById(R.id.serverLabel);
        zcodeHealth = findViewById(R.id.zcodeHealth);
        zcodeSummary = findViewById(R.id.zcodeSummary);
        taskStatus = findViewById(R.id.taskStatus);
        taskTitle = findViewById(R.id.taskTitle);
        taskActivity = findViewById(R.id.taskActivity);
        commandHealth = findViewById(R.id.commandHealth);
        planText = findViewById(R.id.planText);
        creditText = findViewById(R.id.creditText);
        fiveHourLabel = findViewById(R.id.fiveHourLabel);
        weeklyLabel = findViewById(R.id.weeklyLabel);
        fiveHourProgress = findViewById(R.id.fiveHourProgress);
        weeklyProgress = findViewById(R.id.weeklyProgress);
    }

    private void showSetup(String existingUrl) {
        dashboardPanel.setVisibility(View.GONE);
        setupPanel.setVisibility(View.VISIBLE);
        serverUrlInput.setText(existingUrl == null ? "" : existingUrl);
        setupStatus.setText("");
        connectButton.setEnabled(true);
    }

    private void showDashboard() {
        setupPanel.setVisibility(View.GONE);
        dashboardPanel.setVisibility(View.VISIBLE);
        serverLabel.setText(serverUrl);
        liveStatus.setText("CONNECTING");
        liveStatus.setTextColor(Color.rgb(253, 214, 99));
    }

    private void testAndSaveServer() {
        String candidate = normalizeServerUrl(serverUrlInput.getText().toString());
        if (candidate == null) {
            setupStatus.setText("Enter a valid server address");
            return;
        }
        connectButton.setEnabled(false);
        setupStatus.setText("Testing connection…");
        networkExecutor.execute(() -> {
            try {
                client.checkHealth(candidate);
                mainHandler.post(() -> {
                    preferences.edit().putString(KEY_SERVER_URL, candidate).apply();
                    serverUrl = candidate;
                    failureCount = 0;
                    showDashboard();
                    startPolling();
                });
            } catch (Exception error) {
                mainHandler.post(() -> {
                    setupStatus.setText("Connection failed: " + safeError(error));
                    connectButton.setEnabled(true);
                });
            }
        });
    }

    private void startPolling() {
        if (polling) return;
        polling = true;
        mainHandler.removeCallbacks(pollRunnable);
        mainHandler.post(pollRunnable);
    }

    private void stopPolling() {
        polling = false;
        mainHandler.removeCallbacks(pollRunnable);
    }

    private void scheduleNext(long delayMs) {
        if (!polling) return;
        mainHandler.removeCallbacks(pollRunnable);
        mainHandler.postDelayed(pollRunnable, delayMs);
    }

    private void requestState() {
        if (!polling || serverUrl == null || !requestInFlight.compareAndSet(false, true)) return;
        networkExecutor.execute(() -> {
            try {
                StateSnapshot snapshot = client.fetchState(serverUrl);
                mainHandler.post(() -> {
                    requestInFlight.set(false);
                    failureCount = 0;
                    render(snapshot);
                    scheduleNext(POLL_MS);
                });
            } catch (StateSnapshot.IncompatibleSchemaException incompatible) {
                mainHandler.post(() -> {
                    requestInFlight.set(false);
                    polling = false;
                    showCompatibilityError(incompatible.receivedSchema);
                });
            } catch (Exception error) {
                mainHandler.post(() -> {
                    requestInFlight.set(false);
                    failureCount++;
                    showOffline(error);
                    scheduleNext(backoffMillis(failureCount));
                });
            }
        });
    }

    private void render(StateSnapshot state) {
        boolean live = "live".equals(state.overallStatus);
        liveStatus.setText(live ? "● LIVE" : "● DEGRADED");
        liveStatus.setTextColor(live ? Color.rgb(129, 201, 149) : Color.rgb(253, 214, 99));
        serverLabel.setText(serverUrl);
        zcodeHealth.setText("source: " + upper(state.zcodeHealth));
        commandHealth.setText("source: " + upper(state.commandHealth));
        zcodeSummary.setText(formatSummary(state));

        if (state.taskTitle == null) {
            taskStatus.setText("NO TASK DATA");
            taskStatus.setTextColor(Color.rgb(154, 160, 166));
            taskTitle.setText("--");
            taskActivity.setText("");
        } else {
            taskStatus.setText(upper(state.taskStatus));
            taskStatus.setTextColor(taskColor(state.taskStatus));
            taskTitle.setText(state.taskTitle);
            taskActivity.setText(state.taskActivity == null ? "" : state.taskActivity);
        }

        planText.setText("Plan  " + valueOrDash(state.plan));
        creditText.setText(formatCredit(state));
        renderWindow(state.fiveHour, fiveHourLabel, fiveHourProgress, "5H");
        renderWindow(state.weekly, weeklyLabel, weeklyProgress, "WEEK");
    }

    private void renderWindow(StateSnapshot.UsageWindow window, TextView label, ProgressBar progress, String fallbackName) {
        if (window == null || window.usedPercent == null) {
            label.setText(fallbackName + "  --");
            progress.setProgress(0);
            return;
        }
        int percent = Math.max(0, Math.min(100, (int) Math.round(window.usedPercent)));
        String reset = window.resetAt == null ? "" : "  reset " + window.resetAt;
        label.setText(String.format(Locale.US, "%s  %.1f%%%s", fallbackName, window.usedPercent, reset));
        progress.setProgress(percent);
    }

    private String formatSummary(StateSnapshot state) {
        if (state.running == null || state.waiting == null || state.failed == null || state.completed == null) return "Task summary unavailable";
        return String.format(Locale.US, "%d running  ·  %d waiting  ·  %d failed", state.running, state.waiting, state.failed);
    }

    private String formatCredit(StateSnapshot state) {
        if (state.creditRemaining == null) return "Credit --";
        if (state.creditLimit == null) return String.format(Locale.US, "%.2f %s remaining", state.creditRemaining, valueOrDash(state.creditUnit));
        return String.format(Locale.US, "%.2f / %.2f %s", state.creditRemaining, state.creditLimit, valueOrDash(state.creditUnit));
    }

    private void showOffline(Exception error) {
        liveStatus.setText("● OFFLINE");
        liveStatus.setTextColor(Color.rgb(242, 139, 130));
        serverLabel.setText(serverUrl + "  ·  " + safeError(error));
    }

    private void showCompatibilityError(int schema) {
        liveStatus.setText("SCHEMA ERROR");
        liveStatus.setTextColor(Color.rgb(242, 139, 130));
        serverLabel.setText("Backend schema " + schema + " is not supported by this APK");
    }

    private static long backoffMillis(int failures) {
        int exponent = Math.min(4, Math.max(0, failures - 1));
        return Math.min(MAX_BACKOFF_MS, POLL_MS * (1L << exponent));
    }

    private static String normalizeServerUrl(String raw) {
        if (raw == null) return null;
        String value = raw.trim();
        if (value.isEmpty() || value.contains(" ")) return null;
        if (!value.startsWith("http://") && !value.startsWith("https://")) value = "http://" + value;
        while (value.endsWith("/")) value = value.substring(0, value.length() - 1);
        return value;
    }

    private static String safeError(Exception error) {
        if (error instanceof StateSnapshot.IncompatibleSchemaException) return error.getMessage();
        String name = error.getClass().getSimpleName();
        return name.isEmpty() ? "network error" : name;
    }

    private static String upper(String value) {
        return value == null ? "UNKNOWN" : value.toUpperCase(Locale.US);
    }

    private static String valueOrDash(String value) {
        return value == null || value.isEmpty() ? "--" : value;
    }

    private static int taskColor(String status) {
        if ("failed".equals(status)) return Color.rgb(242, 139, 130);
        if ("running".equals(status)) return Color.rgb(129, 201, 149);
        if ("waiting".equals(status)) return Color.rgb(253, 214, 99);
        return Color.rgb(154, 160, 166);
    }

    private void enterImmersiveMode() {
        getWindow().getDecorView().setSystemUiVisibility(View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY | View.SYSTEM_UI_FLAG_FULLSCREEN | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION | View.SYSTEM_UI_FLAG_LAYOUT_STABLE);
    }
}
