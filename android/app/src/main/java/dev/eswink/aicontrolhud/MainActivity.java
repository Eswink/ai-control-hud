package dev.eswink.aicontrolhud;

import android.app.Activity;
import android.content.Context;
import android.content.SharedPreferences;
import android.content.res.ColorStateList;
import android.graphics.Color;
import android.graphics.drawable.GradientDrawable;
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
import android.widget.Switch;
import android.widget.TextView;

import java.io.IOException;
import java.text.SimpleDateFormat;
import java.util.ArrayList;
import java.util.Calendar;
import java.util.Collections;
import java.util.Date;
import java.util.List;
import java.util.Locale;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicBoolean;

public final class MainActivity extends Activity {
    private static final String PREFS = "hud_settings";
    private static final String KEY_SERVER_URL = "server_url";
    private static final String KEY_EVENT_CURSOR = "event_cursor";
    private static final String KEY_EVENT_CURSOR_SERVER = "event_cursor_server";
    private static final String KEY_SPEAK_COMPLETED = "speak_completed";
    private static final String KEY_SPEAK_FAILED = "speak_failed";
    private static final String KEY_QUIET_HOURS = "quiet_hours";

    private static final long POLL_MS = 2000L;
    private static final long MAX_BACKOFF_MS = 30000L;
    private static final long COUNTDOWN_TICK_MS = 1000L;
    private static final int MAX_TASK_ROWS = 4;
    private static final int EVENT_PAGE_LIMIT = 100;

    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final ExecutorService networkExecutor = Executors.newSingleThreadExecutor();
    private final AtomicBoolean requestInFlight = new AtomicBoolean(false);
    private final StateClient client = new StateClient();
    private final ArrayList<TaskRow> taskRows = new ArrayList<>();

    private SharedPreferences preferences;
    private VoiceNotifier voiceNotifier;
    private boolean polling;
    private int failureCount;
    private String serverUrl;
    private StateSnapshot lastSnapshot;
    private String lastEventStatus = "Events not synchronized";
    private boolean oldEventsPendingSummary;

    private LinearLayout setupPanel;
    private ScrollView dashboardPanel;
    private EditText serverUrlInput;
    private Button connectButton;
    private TextView setupStatus;
    private TextView liveStatus;
    private TextView serverLabel;
    private TextView lastUpdateText;
    private TextView zcodeHealth;
    private TextView zcodeSummary;
    private LinearLayout taskList;
    private TextView taskEmpty;
    private TextView taskOverflow;
    private TextView commandHealth;
    private TextView planText;
    private TextView creditText;
    private TextView fiveHourLabel;
    private TextView weeklyLabel;
    private ProgressBar fiveHourProgress;
    private ProgressBar weeklyProgress;
    private Switch completedVoiceSwitch;
    private Switch failedVoiceSwitch;
    private Switch quietHoursSwitch;
    private TextView voiceStatusText;
    private Button testVoiceButton;

    private final Runnable pollRunnable = this::requestState;
    private final Runnable countdownRunnable = new Runnable() {
        @Override
        public void run() {
            if (!polling) return;
            if (lastSnapshot != null) renderUsageWindows(lastSnapshot);
            mainHandler.postDelayed(this, COUNTDOWN_TICK_MS);
        }
    };

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        setContentView(R.layout.activity_main);
        bindViews();
        preferences = getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        loadVoicePreferences();

        voiceNotifier = new VoiceNotifier(
                this,
                () -> mainHandler.post(this::renderVoiceStatus)
        );

        connectButton.setOnClickListener(view -> testAndSaveServer());
        findViewById(R.id.changeServerButton).setOnClickListener(view -> {
            stopPolling();
            showSetup(serverUrl);
        });
        completedVoiceSwitch.setOnCheckedChangeListener((button, checked) -> {
            preferences.edit().putBoolean(KEY_SPEAK_COMPLETED, checked).apply();
            renderVoiceStatus();
        });
        failedVoiceSwitch.setOnCheckedChangeListener((button, checked) -> {
            preferences.edit().putBoolean(KEY_SPEAK_FAILED, checked).apply();
            renderVoiceStatus();
        });
        quietHoursSwitch.setOnCheckedChangeListener((button, checked) -> {
            preferences.edit().putBoolean(KEY_QUIET_HOURS, checked).apply();
            renderVoiceStatus();
        });
        testVoiceButton.setOnClickListener(view -> voiceNotifier.speak(
                NotificationPolicy.testSpeech(Locale.getDefault())
        ));
        renderVoiceStatus();
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
        if (voiceNotifier != null) voiceNotifier.shutdown();
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
        lastUpdateText = findViewById(R.id.lastUpdateText);
        zcodeHealth = findViewById(R.id.zcodeHealth);
        zcodeSummary = findViewById(R.id.zcodeSummary);
        taskList = findViewById(R.id.taskList);
        taskEmpty = findViewById(R.id.taskEmpty);
        taskOverflow = findViewById(R.id.taskOverflow);
        commandHealth = findViewById(R.id.commandHealth);
        planText = findViewById(R.id.planText);
        creditText = findViewById(R.id.creditText);
        fiveHourLabel = findViewById(R.id.fiveHourLabel);
        weeklyLabel = findViewById(R.id.weeklyLabel);
        fiveHourProgress = findViewById(R.id.fiveHourProgress);
        weeklyProgress = findViewById(R.id.weeklyProgress);
        completedVoiceSwitch = findViewById(R.id.completedVoiceSwitch);
        failedVoiceSwitch = findViewById(R.id.failedVoiceSwitch);
        quietHoursSwitch = findViewById(R.id.quietHoursSwitch);
        voiceStatusText = findViewById(R.id.voiceStatusText);
        testVoiceButton = findViewById(R.id.testVoiceButton);
    }

    private void loadVoicePreferences() {
        completedVoiceSwitch.setChecked(preferences.getBoolean(KEY_SPEAK_COMPLETED, true));
        failedVoiceSwitch.setChecked(preferences.getBoolean(KEY_SPEAK_FAILED, true));
        quietHoursSwitch.setChecked(preferences.getBoolean(KEY_QUIET_HOURS, true));
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
        lastUpdateText.setText("Waiting for first snapshot");
        liveStatus.setText("CONNECTING");
        liveStatus.setTextColor(Color.rgb(253, 214, 99));
        renderVoiceStatus();
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
                    String previousServer = preferences.getString(KEY_SERVER_URL, null);
                    SharedPreferences.Editor editor = preferences.edit().putString(KEY_SERVER_URL, candidate);
                    if (!candidate.equals(previousServer)) {
                        editor.remove(KEY_EVENT_CURSOR).remove(KEY_EVENT_CURSOR_SERVER);
                        oldEventsPendingSummary = false;
                        lastEventStatus = "Events baseline pending";
                    }
                    editor.apply();
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
        mainHandler.removeCallbacks(countdownRunnable);
        mainHandler.post(pollRunnable);
        mainHandler.post(countdownRunnable);
    }

    private void stopPolling() {
        polling = false;
        mainHandler.removeCallbacks(pollRunnable);
        mainHandler.removeCallbacks(countdownRunnable);
    }

    private void scheduleNext(long delayMs) {
        if (!polling) return;
        mainHandler.removeCallbacks(pollRunnable);
        mainHandler.postDelayed(pollRunnable, delayMs);
    }

    private void requestState() {
        if (!polling || serverUrl == null || !requestInFlight.compareAndSet(false, true)) return;
        String targetServer = serverUrl;
        networkExecutor.execute(() -> {
            final StateSnapshot snapshot;
            try {
                snapshot = client.fetchState(targetServer);
            } catch (StateSnapshot.IncompatibleSchemaException incompatible) {
                mainHandler.post(() -> {
                    requestInFlight.set(false);
                    polling = false;
                    mainHandler.removeCallbacks(countdownRunnable);
                    showCompatibilityError(incompatible.receivedSchema);
                });
                return;
            } catch (Exception error) {
                mainHandler.post(() -> {
                    requestInFlight.set(false);
                    if (!polling || !targetServer.equals(serverUrl)) return;
                    failureCount++;
                    showOffline(error);
                    scheduleNext(backoffMillis(failureCount));
                });
                return;
            }

            EventSyncResult eventSync = null;
            Exception eventError = null;
            try {
                eventSync = syncEvents(targetServer);
            } catch (Exception error) {
                eventError = error;
            }

            EventSyncResult completedEventSync = eventSync;
            Exception completedEventError = eventError;
            mainHandler.post(() -> {
                requestInFlight.set(false);
                if (!polling || !targetServer.equals(serverUrl)) return;
                failureCount = 0;
                render(snapshot);
                if (completedEventSync != null) {
                    handleEventSync(completedEventSync);
                } else if (completedEventError != null) {
                    handleEventError(completedEventError);
                }
                scheduleNext(POLL_MS);
            });
        });
    }

    private EventSyncResult syncEvents(String targetServer) throws Exception {
        String cursorServer = preferences.getString(KEY_EVENT_CURSOR_SERVER, null);
        boolean hasCursor = targetServer.equals(cursorServer) && preferences.contains(KEY_EVENT_CURSOR);
        long after = hasCursor ? Math.max(0L, preferences.getLong(KEY_EVENT_CURSOR, 0L)) : 0L;
        EventPage page = client.fetchEvents(targetServer, after, hasCursor ? EVENT_PAGE_LIMIT : 1);
        boolean rebased = hasCursor && page.requiresRebase(after);
        boolean baseline = !hasCursor || rebased;
        long nextCursor = baseline ? page.latestSeq : page.nextAfter;

        boolean saved = preferences.edit()
                .putString(KEY_EVENT_CURSOR_SERVER, targetServer)
                .putLong(KEY_EVENT_CURSOR, nextCursor)
                .commit();
        if (!saved) throw new IOException("event cursor persistence failed");
        return new EventSyncResult(page, baseline, rebased, nextCursor);
    }

    private void handleEventSync(EventSyncResult sync) {
        if (sync.baseline) {
            oldEventsPendingSummary = false;
            lastEventStatus = (sync.rebased ? "Events rebased" : "Events baseline") + " · #" + sync.cursor;
            renderVoiceStatus();
            return;
        }

        long nowMillis = System.currentTimeMillis();
        int minuteOfDay = localMinuteOfDay();
        boolean quietHoursEnabled = quietHoursSwitch.isChecked();
        boolean quietNow = quietHoursEnabled && NotificationPolicy.isQuietMinute(minuteOfDay);

        for (EventPage.EventItem event : sync.page.events) {
            boolean voiceEnabled = voiceEnabledFor(event);
            if (NotificationPolicy.shouldSpeak(
                    event,
                    voiceEnabled,
                    quietHoursEnabled,
                    minuteOfDay,
                    nowMillis
            )) {
                voiceNotifier.speak(NotificationPolicy.speechText(event, Locale.getDefault()));
            } else if (voiceEnabled && !quietNow && NotificationPolicy.isTooOld(event, nowMillis)) {
                oldEventsPendingSummary = true;
            }
        }

        boolean caughtUp = sync.page.nextAfter >= sync.page.latestSeq;
        if (caughtUp && oldEventsPendingSummary && !quietNow) {
            voiceNotifier.speak(NotificationPolicy.offlineSummaryText(Locale.getDefault()));
            oldEventsPendingSummary = false;
        }

        lastEventStatus = caughtUp
                ? "Events synced · #" + sync.cursor
                : "Events catching up · #" + sync.cursor + " / #" + sync.page.latestSeq;
        renderVoiceStatus();
    }

    private void handleEventError(Exception error) {
        if (error instanceof EventPage.IncompatibleSchemaException) {
            EventPage.IncompatibleSchemaException incompatible = (EventPage.IncompatibleSchemaException) error;
            lastEventStatus = "Event schema " + incompatible.receivedSchema + " unsupported";
        } else {
            String name = error.getClass().getSimpleName();
            lastEventStatus = "Event sync error · " + (name.isEmpty() ? "network" : name);
        }
        renderVoiceStatus();
    }

    private boolean voiceEnabledFor(EventPage.EventItem event) {
        if (event == null) return false;
        if ("task.completed".equals(event.type)) return completedVoiceSwitch.isChecked();
        if ("task.failed".equals(event.type)) return failedVoiceSwitch.isChecked();
        return false;
    }

    private int localMinuteOfDay() {
        Calendar calendar = Calendar.getInstance();
        return calendar.get(Calendar.HOUR_OF_DAY) * 60 + calendar.get(Calendar.MINUTE);
    }

    private void renderVoiceStatus() {
        if (voiceStatusText == null || testVoiceButton == null) return;
        boolean ready = voiceNotifier != null && voiceNotifier.isReady();
        String tts = ready ? "TTS READY" : "TTS STARTING / UNAVAILABLE";
        String policy = "complete " + onOff(completedVoiceSwitch != null && completedVoiceSwitch.isChecked())
                + " · fail " + onOff(failedVoiceSwitch != null && failedVoiceSwitch.isChecked())
                + " · quiet " + onOff(quietHoursSwitch != null && quietHoursSwitch.isChecked());
        voiceStatusText.setText(tts + " · " + lastEventStatus + "\n" + policy);
        testVoiceButton.setEnabled(ready);
    }

    private void render(StateSnapshot state) {
        lastSnapshot = state;
        boolean live = "live".equals(state.overallStatus);
        liveStatus.setText(live ? "● LIVE" : "● DEGRADED");
        liveStatus.setTextColor(live ? Color.rgb(129, 201, 149) : Color.rgb(253, 214, 99));
        serverLabel.setText(serverUrl);
        lastUpdateText.setText("Updated " + new SimpleDateFormat("HH:mm:ss", Locale.US).format(new Date()));

        renderSourceHealth(zcodeHealth, state.zcodeHealth, state.zcodeMessage);
        renderSourceHealth(commandHealth, state.commandHealth, state.commandMessage);
        zcodeSummary.setText(formatSummary(state));
        renderTasks(state.tasks);

        planText.setText("Plan  " + valueOrDash(state.plan));
        creditText.setText(formatCredit(state));
        renderUsageWindows(state);
    }

    private void renderSourceHealth(TextView view, String status, String message) {
        String text = "● " + upper(status);
        if (message != null && !message.isEmpty()) text += " · " + message;
        view.setText(text);
        view.setTextColor(sourceHealthColor(status));
    }

    private void renderTasks(List<StateSnapshot.TaskItem> tasks) {
        if (tasks == null) {
            hideAllTaskRows();
            taskEmpty.setVisibility(View.VISIBLE);
            taskEmpty.setText("Task data unavailable");
            taskOverflow.setVisibility(View.GONE);
            return;
        }

        if (tasks.isEmpty()) {
            hideAllTaskRows();
            taskEmpty.setVisibility(View.VISIBLE);
            taskEmpty.setText("No tasks reported");
            taskOverflow.setVisibility(View.GONE);
            return;
        }

        taskEmpty.setVisibility(View.GONE);
        ArrayList<StateSnapshot.TaskItem> display = new ArrayList<>(tasks);
        Collections.sort(display, (left, right) -> Integer.compare(taskPriority(left.status), taskPriority(right.status)));

        int visibleCount = Math.min(MAX_TASK_ROWS, display.size());
        ensureTaskRows(visibleCount);
        for (int i = 0; i < taskRows.size(); i++) {
            TaskRow row = taskRows.get(i);
            if (i < visibleCount) {
                renderTaskRow(row, display.get(i));
                row.container.setVisibility(View.VISIBLE);
            } else {
                row.container.setVisibility(View.GONE);
            }
        }

        int hidden = display.size() - visibleCount;
        if (hidden > 0) {
            taskOverflow.setText("+" + hidden + " more task" + (hidden == 1 ? "" : "s"));
            taskOverflow.setVisibility(View.VISIBLE);
        } else {
            taskOverflow.setVisibility(View.GONE);
        }
    }

    private void ensureTaskRows(int count) {
        while (taskRows.size() < count) {
            TaskRow row = createTaskRow();
            taskRows.add(row);
            taskList.addView(row.container);
        }
    }

    private TaskRow createTaskRow() {
        LinearLayout container = new LinearLayout(this);
        container.setOrientation(LinearLayout.VERTICAL);
        container.setPadding(dp(12), dp(10), dp(12), dp(10));

        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
        );
        params.topMargin = dp(8);
        container.setLayoutParams(params);

        GradientDrawable background = new GradientDrawable();
        background.setColor(Color.rgb(16, 23, 32));
        background.setCornerRadius(dp(10));
        background.setStroke(dp(1), Color.rgb(39, 49, 61));
        container.setBackground(background);

        TextView status = new TextView(this);
        status.setTextSize(11);
        status.setTypeface(null, android.graphics.Typeface.BOLD);

        TextView title = new TextView(this);
        title.setTextColor(Color.rgb(241, 243, 244));
        title.setTextSize(17);
        title.setMaxLines(2);
        LinearLayout.LayoutParams titleParams = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
        );
        titleParams.topMargin = dp(3);
        title.setLayoutParams(titleParams);

        TextView meta = new TextView(this);
        meta.setTextColor(Color.rgb(154, 160, 166));
        meta.setTextSize(12);
        meta.setMaxLines(3);
        LinearLayout.LayoutParams metaParams = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
        );
        metaParams.topMargin = dp(4);
        meta.setLayoutParams(metaParams);

        container.addView(status);
        container.addView(title);
        container.addView(meta);
        return new TaskRow(container, status, title, meta);
    }

    private void renderTaskRow(TaskRow row, StateSnapshot.TaskItem task) {
        row.status.setText(upper(task.status));
        row.status.setTextColor(taskColor(task.status));
        row.title.setText(task.title);
        row.meta.setText(formatTaskMeta(task));
    }

    private String formatTaskMeta(StateSnapshot.TaskItem task) {
        ArrayList<String> parts = new ArrayList<>();
        if (task.workspace != null) parts.add(task.workspace);
        if (task.durationSeconds != null) parts.add(formatDuration(task.durationSeconds));
        if (task.additions != null || task.deletions != null) {
            String additions = task.additions == null ? "?" : String.valueOf(task.additions);
            String deletions = task.deletions == null ? "?" : String.valueOf(task.deletions);
            parts.add("+" + additions + " / -" + deletions);
        }
        String meta = join(parts, " · ");
        if (task.activity != null) {
            if (!meta.isEmpty()) meta += "\n";
            meta += "> " + task.activity;
        }
        return meta;
    }

    private void hideAllTaskRows() {
        for (TaskRow row : taskRows) row.container.setVisibility(View.GONE);
    }

    private void renderUsageWindows(StateSnapshot state) {
        renderWindow(state.fiveHour, fiveHourLabel, fiveHourProgress, "5H");
        renderWindow(state.weekly, weeklyLabel, weeklyProgress, "WEEK");
    }

    private void renderWindow(
            StateSnapshot.UsageWindow window,
            TextView label,
            ProgressBar progress,
            String fallbackName
    ) {
        if (window == null || window.usedPercent == null) {
            label.setText(fallbackName + "  --");
            progress.setProgress(0);
            progress.setProgressTintList(ColorStateList.valueOf(Color.rgb(95, 99, 104)));
            return;
        }

        int percent = Math.max(0, Math.min(100, (int) Math.round(window.usedPercent)));
        String reset = window.resetAt == null ? "" : " · reset " + formatResetCountdown(window.resetAt);
        label.setText(String.format(Locale.US, "%s  %.1f%%%s", fallbackName, window.usedPercent, reset));
        progress.setProgress(percent);
        progress.setProgressTintList(ColorStateList.valueOf(usageColor(window.usedPercent)));
    }

    private String formatSummary(StateSnapshot state) {
        if (state.running == null || state.waiting == null || state.failed == null || state.completed == null) {
            return "Task summary unavailable";
        }
        return String.format(
                Locale.US,
                "%d running  ·  %d waiting  ·  %d failed",
                state.running,
                state.waiting,
                state.failed
        );
    }

    private String formatCredit(StateSnapshot state) {
        if (state.creditRemaining == null) return "Credit --";
        if (state.creditLimit == null) {
            return String.format(
                    Locale.US,
                    "%.2f %s remaining",
                    state.creditRemaining,
                    valueOrDash(state.creditUnit)
            );
        }
        return String.format(
                Locale.US,
                "%.2f / %.2f %s",
                state.creditRemaining,
                state.creditLimit,
                valueOrDash(state.creditUnit)
        );
    }

    private void showOffline(Exception error) {
        if (error instanceof StateClient.SnapshotUnavailableException) {
            failureCount = 0;
            lastSnapshot = null;
            liveStatus.setText("● WAITING FOR AGENT");
            liveStatus.setTextColor(Color.rgb(253, 214, 99));
            serverLabel.setText(serverUrl);
            lastUpdateText.setText("Hub online · waiting for Windows Agent snapshot");

            zcodeHealth.setText("● WAITING · no Agent snapshot");
            zcodeHealth.setTextColor(Color.rgb(253, 214, 99));
            zcodeSummary.setText("Waiting for Windows Agent snapshot");
            hideAllTaskRows();
            taskEmpty.setVisibility(View.VISIBLE);
            taskEmpty.setText("Waiting for Agent snapshot");
            taskOverflow.setVisibility(View.GONE);

            commandHealth.setText("● WAITING · no Agent snapshot");
            commandHealth.setTextColor(Color.rgb(253, 214, 99));
            planText.setText("Plan --");
            creditText.setText("Credit --");
            fiveHourLabel.setText("5H --");
            weeklyLabel.setText("WEEK --");
            fiveHourProgress.setProgress(0);
            weeklyProgress.setProgress(0);
            fiveHourProgress.setProgressTintList(ColorStateList.valueOf(Color.rgb(95, 99, 104)));
            weeklyProgress.setProgressTintList(ColorStateList.valueOf(Color.rgb(95, 99, 104)));
            return;
        }

        if (error instanceof StateClient.HttpStatusException) {
            StateClient.HttpStatusException status = (StateClient.HttpStatusException) error;
            liveStatus.setText("● SERVER ERROR");
            liveStatus.setTextColor(Color.rgb(242, 139, 130));
            serverLabel.setText(serverUrl + " · HTTP " + status.statusCode);
            return;
        }

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
        if (error instanceof StateClient.HttpStatusException) {
            return "HTTP " + ((StateClient.HttpStatusException) error).statusCode;
        }
        String name = error.getClass().getSimpleName();
        return name.isEmpty() ? "network error" : name;
    }

    private static String upper(String value) {
        return value == null ? "UNKNOWN" : value.toUpperCase(Locale.US);
    }

    private static String valueOrDash(String value) {
        return value == null || value.isEmpty() ? "--" : value;
    }

    private static String onOff(boolean enabled) {
        return enabled ? "on" : "off";
    }

    private static int taskPriority(String status) {
        if ("failed".equals(status)) return 0;
        if ("running".equals(status)) return 1;
        if ("waiting".equals(status)) return 2;
        if ("unknown".equals(status)) return 3;
        if ("completed".equals(status)) return 4;
        return 5;
    }

    private static int taskColor(String status) {
        if ("failed".equals(status)) return Color.rgb(242, 139, 130);
        if ("running".equals(status)) return Color.rgb(129, 201, 149);
        if ("waiting".equals(status)) return Color.rgb(253, 214, 99);
        return Color.rgb(154, 160, 166);
    }

    private static int sourceHealthColor(String status) {
        if ("ok".equals(status)) return Color.rgb(129, 201, 149);
        if ("stale".equals(status)) return Color.rgb(253, 214, 99);
        if ("error".equals(status)) return Color.rgb(242, 139, 130);
        return Color.rgb(154, 160, 166);
    }

    private static int usageColor(double percent) {
        if (percent >= 90.0) return Color.rgb(242, 139, 130);
        if (percent >= 70.0) return Color.rgb(253, 214, 99);
        return Color.rgb(138, 180, 248);
    }

    private static String formatDuration(int totalSeconds) {
        int safe = Math.max(0, totalSeconds);
        int hours = safe / 3600;
        int minutes = (safe % 3600) / 60;
        int seconds = safe % 60;
        if (hours > 0) return String.format(Locale.US, "%dh %02dm", hours, minutes);
        if (minutes > 0) return String.format(Locale.US, "%dm %02ds", minutes, seconds);
        return seconds + "s";
    }

    private static String formatResetCountdown(String isoTimestamp) {
        Long resetMillis = IsoTime.parseMillis(isoTimestamp);
        if (resetMillis == null) return "--";
        long remainingSeconds = Math.max(0L, (resetMillis - System.currentTimeMillis()) / 1000L);
        long days = remainingSeconds / 86400L;
        long hours = (remainingSeconds % 86400L) / 3600L;
        long minutes = (remainingSeconds % 3600L) / 60L;
        long seconds = remainingSeconds % 60L;
        if (days > 0) return String.format(Locale.US, "%dd %02dh", days, hours);
        return String.format(Locale.US, "%02d:%02d:%02d", hours, minutes, seconds);
    }

    private static String join(List<String> values, String separator) {
        StringBuilder result = new StringBuilder();
        for (String value : values) {
            if (result.length() > 0) result.append(separator);
            result.append(value);
        }
        return result.toString();
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private void enterImmersiveMode() {
        getWindow().getDecorView().setSystemUiVisibility(
                View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                        | View.SYSTEM_UI_FLAG_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                        | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                        | View.SYSTEM_UI_FLAG_LAYOUT_STABLE
        );
    }

    private static final class EventSyncResult {
        final EventPage page;
        final boolean baseline;
        final boolean rebased;
        final long cursor;

        EventSyncResult(EventPage page, boolean baseline, boolean rebased, long cursor) {
            this.page = page;
            this.baseline = baseline;
            this.rebased = rebased;
            this.cursor = cursor;
        }
    }

    private static final class TaskRow {
        final LinearLayout container;
        final TextView status;
        final TextView title;
        final TextView meta;

        TaskRow(LinearLayout container, TextView status, TextView title, TextView meta) {
            this.container = container;
            this.status = status;
            this.title = title;
            this.meta = meta;
        }
    }
}
