package dev.eswink.aicontrolhud;

import android.app.Activity;
import android.content.Context;
import android.content.SharedPreferences;
import android.content.res.ColorStateList;
import android.graphics.drawable.GradientDrawable;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.util.TypedValue;
import android.view.View;
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
    private volatile boolean destroyed;
    private boolean polling;
    private int failureCount;
    private String serverUrl;
    private StateSnapshot lastSnapshot;
    private String lastEventStatus;
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
            if (!polling || destroyed) return;
            if (lastSnapshot != null) renderUsageWindows(lastSnapshot);
            mainHandler.postDelayed(this, COUNTDOWN_TICK_MS);
        }
    };

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_main);
        bindViews();
        preferences = getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        lastEventStatus = getString(R.string.events_not_synchronized);
        loadVoicePreferences();

        voiceNotifier = new VoiceNotifier(
                this,
                () -> postToUi(this::renderVoiceStatus)
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
        destroyed = true;
        stopPolling();
        mainHandler.removeCallbacksAndMessages(null);
        requestInFlight.set(false);
        if (voiceNotifier != null) voiceNotifier.shutdown();
        networkExecutor.shutdownNow();
        super.onDestroy();
    }

    private void postToUi(Runnable action) {
        if (destroyed) return;
        mainHandler.post(() -> {
            if (!destroyed) action.run();
        });
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
        lastUpdateText.setText(R.string.waiting_first_snapshot);
        liveStatus.setText(R.string.status_connecting);
        liveStatus.setTextColor(color(R.color.hud_warning));
        renderVoiceStatus();
    }

    private void testAndSaveServer() {
        String candidate = normalizeServerUrl(serverUrlInput.getText().toString());
        if (candidate == null) {
            setupStatus.setText(R.string.enter_valid_server);
            return;
        }
        connectButton.setEnabled(false);
        setupStatus.setText(R.string.testing_connection);
        networkExecutor.execute(() -> {
            try {
                client.checkHealth(candidate);
                postToUi(() -> {
                    String previousServer = preferences.getString(KEY_SERVER_URL, null);
                    SharedPreferences.Editor editor = preferences.edit().putString(KEY_SERVER_URL, candidate);
                    if (!candidate.equals(previousServer)) {
                        editor.remove(KEY_EVENT_CURSOR).remove(KEY_EVENT_CURSOR_SERVER);
                        oldEventsPendingSummary = false;
                        lastEventStatus = getString(R.string.events_baseline_pending);
                    }
                    editor.apply();
                    serverUrl = candidate;
                    failureCount = 0;
                    showDashboard();
                    startPolling();
                });
            } catch (Exception error) {
                postToUi(() -> {
                    setupStatus.setText(getString(R.string.connection_failed, safeError(error)));
                    connectButton.setEnabled(true);
                });
            }
        });
    }

    private void startPolling() {
        if (polling || destroyed) return;
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
        if (!polling || destroyed) return;
        mainHandler.removeCallbacks(pollRunnable);
        mainHandler.postDelayed(pollRunnable, delayMs);
    }

    private void requestState() {
        if (destroyed || !polling || serverUrl == null || !requestInFlight.compareAndSet(false, true)) return;
        String targetServer = serverUrl;
        networkExecutor.execute(() -> {
            final StateSnapshot snapshot;
            try {
                snapshot = client.fetchState(targetServer);
            } catch (StateSnapshot.IncompatibleSchemaException incompatible) {
                postToUi(() -> {
                    requestInFlight.set(false);
                    polling = false;
                    mainHandler.removeCallbacks(countdownRunnable);
                    showCompatibilityError(incompatible.receivedSchema);
                });
                return;
            } catch (Exception error) {
                postToUi(() -> {
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
            postToUi(() -> {
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
        if (!saved) throw new IOException(getString(R.string.cursor_persistence_failed));
        return new EventSyncResult(page, baseline, rebased, nextCursor);
    }

    private void handleEventSync(EventSyncResult sync) {
        if (sync.baseline) {
            oldEventsPendingSummary = false;
            lastEventStatus = getString(
                    sync.rebased ? R.string.events_rebased : R.string.events_baseline,
                    sync.cursor
            );
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
                ? getString(R.string.events_synced, sync.cursor)
                : getString(R.string.events_catching_up, sync.cursor, sync.page.latestSeq);
        renderVoiceStatus();
    }

    private void handleEventError(Exception error) {
        if (error instanceof EventPage.IncompatibleSchemaException) {
            EventPage.IncompatibleSchemaException incompatible = (EventPage.IncompatibleSchemaException) error;
            lastEventStatus = getString(R.string.event_schema_unsupported, incompatible.receivedSchema);
        } else {
            String name = error.getClass().getSimpleName();
            lastEventStatus = getString(
                    R.string.event_sync_error,
                    name.isEmpty() ? getString(R.string.network_error) : name
            );
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
        if (destroyed || voiceStatusText == null || testVoiceButton == null) return;
        boolean ready = voiceNotifier != null && voiceNotifier.isReady();
        String tts = getString(ready ? R.string.tts_ready : R.string.tts_unavailable);
        String policy = getString(
                R.string.voice_policy_format,
                onOff(completedVoiceSwitch != null && completedVoiceSwitch.isChecked()),
                onOff(failedVoiceSwitch != null && failedVoiceSwitch.isChecked()),
                onOff(quietHoursSwitch != null && quietHoursSwitch.isChecked())
        );
        voiceStatusText.setText(getString(R.string.voice_status_format, tts, lastEventStatus, policy));
        testVoiceButton.setEnabled(ready);
    }

    private void render(StateSnapshot state) {
        lastSnapshot = state;
        boolean live = "live".equals(state.overallStatus);
        liveStatus.setText(live ? R.string.status_live : R.string.status_degraded);
        liveStatus.setTextColor(color(live ? R.color.hud_success : R.color.hud_warning));
        serverLabel.setText(serverUrl);
        String time = new SimpleDateFormat("HH:mm:ss", Locale.getDefault()).format(new Date());
        lastUpdateText.setText(getString(R.string.updated_at, time));

        renderSourceHealth(zcodeHealth, state.zcodeHealth, state.zcodeMessage);
        renderSourceHealth(commandHealth, state.commandHealth, state.commandMessage);
        zcodeSummary.setText(formatSummary(state));
        renderTasks(state.tasks);

        planText.setText(state.plan == null || state.plan.isEmpty()
                ? getString(R.string.plan_unavailable)
                : getString(R.string.plan_format, state.plan));
        creditText.setText(formatCredit(state));
        renderUsageWindows(state);
    }

    private void renderSourceHealth(TextView view, String status, String message) {
        String label = sourceStatusLabel(status);
        view.setText(message == null || message.isEmpty()
                ? getString(R.string.source_health_format, label)
                : getString(R.string.source_health_message_format, label, message));
        view.setTextColor(sourceHealthColor(status));
    }

    private void renderTasks(List<StateSnapshot.TaskItem> tasks) {
        if (tasks == null) {
            hideAllTaskRows();
            taskEmpty.setVisibility(View.VISIBLE);
            taskEmpty.setText(R.string.task_data_unavailable);
            taskOverflow.setVisibility(View.GONE);
            return;
        }

        if (tasks.isEmpty()) {
            hideAllTaskRows();
            taskEmpty.setVisibility(View.VISIBLE);
            taskEmpty.setText(R.string.task_none_reported);
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
            taskOverflow.setText(getResources().getQuantityString(R.plurals.task_more, hidden, hidden));
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
        container.setPadding(
                dimenPx(R.dimen.hud_space_12),
                dimenPx(R.dimen.hud_space_10),
                dimenPx(R.dimen.hud_space_12),
                dimenPx(R.dimen.hud_space_10)
        );

        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
        );
        params.topMargin = dimenPx(R.dimen.hud_space_8);
        container.setLayoutParams(params);

        GradientDrawable background = new GradientDrawable();
        background.setColor(color(R.color.hud_surface));
        background.setCornerRadius(dimenPx(R.dimen.hud_space_10));
        background.setStroke(dimenPx(R.dimen.hud_divider_height), color(R.color.hud_border));
        container.setBackground(background);

        TextView status = new TextView(this);
        setTextSize(status, R.dimen.hud_text_11);
        status.setTypeface(null, android.graphics.Typeface.BOLD);

        TextView title = new TextView(this);
        title.setTextColor(color(R.color.hud_text_primary));
        setTextSize(title, R.dimen.hud_text_17);
        title.setMaxLines(2);
        LinearLayout.LayoutParams titleParams = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
        );
        titleParams.topMargin = dimenPx(R.dimen.hud_space_3);
        title.setLayoutParams(titleParams);

        TextView meta = new TextView(this);
        meta.setTextColor(color(R.color.hud_text_secondary));
        setTextSize(meta, R.dimen.hud_text_12);
        meta.setMaxLines(3);
        LinearLayout.LayoutParams metaParams = new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT
        );
        metaParams.topMargin = dimenPx(R.dimen.hud_space_4);
        meta.setLayoutParams(metaParams);

        container.addView(status);
        container.addView(title);
        container.addView(meta);
        return new TaskRow(container, status, title, meta);
    }

    private void renderTaskRow(TaskRow row, StateSnapshot.TaskItem task) {
        row.status.setText(taskStatusLabel(task.status));
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
            parts.add(getString(R.string.task_changes_format, additions, deletions));
        }
        String meta = join(parts, " · ");
        if (task.activity != null) {
            if (!meta.isEmpty()) meta += "\n";
            meta += getString(R.string.task_activity_format, task.activity);
        }
        return meta;
    }

    private void hideAllTaskRows() {
        for (TaskRow row : taskRows) row.container.setVisibility(View.GONE);
    }

    private void renderUsageWindows(StateSnapshot state) {
        renderWindow(state.fiveHour, fiveHourLabel, fiveHourProgress, getString(R.string.usage_window_5h));
        renderWindow(state.weekly, weeklyLabel, weeklyProgress, getString(R.string.usage_window_weekly));
    }

    private void renderWindow(
            StateSnapshot.UsageWindow window,
            TextView label,
            ProgressBar progress,
            String fallbackName
    ) {
        if (window == null || window.usedPercent == null) {
            label.setText(getString(R.string.usage_unavailable, fallbackName));
            progress.setProgress(0);
            progress.setProgressTintList(ColorStateList.valueOf(color(R.color.hud_progress_muted)));
            return;
        }

        int percent = Math.max(0, Math.min(100, (int) Math.round(window.usedPercent)));
        String reset = window.resetAt == null
                ? ""
                : getString(R.string.usage_reset_suffix, formatResetCountdown(window.resetAt));
        label.setText(getString(R.string.usage_format, fallbackName, window.usedPercent, reset));
        progress.setProgress(percent);
        progress.setProgressTintList(ColorStateList.valueOf(usageColor(window.usedPercent)));
    }

    private String formatSummary(StateSnapshot state) {
        if (state.running == null || state.waiting == null || state.failed == null || state.completed == null) {
            return getString(R.string.task_summary_unavailable);
        }
        return getString(R.string.task_summary_format, state.running, state.waiting, state.failed);
    }

    private String formatCredit(StateSnapshot state) {
        if (state.creditRemaining == null) return getString(R.string.credit_unavailable);
        String unit = valueOrDash(state.creditUnit);
        if (state.creditLimit == null) {
            return getString(R.string.credit_remaining_format, state.creditRemaining, unit);
        }
        return getString(R.string.credit_limit_format, state.creditRemaining, state.creditLimit, unit);
    }

    private void showOffline(Exception error) {
        if (error instanceof StateClient.SnapshotUnavailableException) {
            failureCount = 0;
            lastSnapshot = null;
            liveStatus.setText(R.string.status_waiting_agent);
            liveStatus.setTextColor(color(R.color.hud_warning));
            serverLabel.setText(serverUrl);
            lastUpdateText.setText(R.string.hub_waiting_detail);

            zcodeHealth.setText(R.string.source_waiting_snapshot);
            zcodeHealth.setTextColor(color(R.color.hud_warning));
            zcodeSummary.setText(R.string.waiting_agent_summary);
            hideAllTaskRows();
            taskEmpty.setVisibility(View.VISIBLE);
            taskEmpty.setText(R.string.waiting_agent_task);
            taskOverflow.setVisibility(View.GONE);

            commandHealth.setText(R.string.source_waiting_snapshot);
            commandHealth.setTextColor(color(R.color.hud_warning));
            planText.setText(R.string.plan_unavailable);
            creditText.setText(R.string.credit_unavailable);
            fiveHourLabel.setText(R.string.five_hour_unavailable);
            weeklyLabel.setText(R.string.weekly_unavailable);
            fiveHourProgress.setProgress(0);
            weeklyProgress.setProgress(0);
            fiveHourProgress.setProgressTintList(ColorStateList.valueOf(color(R.color.hud_progress_muted)));
            weeklyProgress.setProgressTintList(ColorStateList.valueOf(color(R.color.hud_progress_muted)));
            return;
        }

        if (error instanceof StateClient.HttpStatusException) {
            StateClient.HttpStatusException status = (StateClient.HttpStatusException) error;
            liveStatus.setText(R.string.status_server_error);
            liveStatus.setTextColor(color(R.color.hud_error));
            serverLabel.setText(getString(R.string.server_http_error, serverUrl, status.statusCode));
            return;
        }

        liveStatus.setText(R.string.status_offline);
        liveStatus.setTextColor(color(R.color.hud_error));
        serverLabel.setText(getString(R.string.server_network_error, serverUrl, safeError(error)));
    }

    private void showCompatibilityError(int schema) {
        liveStatus.setText(R.string.status_schema_error);
        liveStatus.setTextColor(color(R.color.hud_error));
        serverLabel.setText(getString(R.string.backend_schema_unsupported, schema));
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

    private String safeError(Exception error) {
        if (error instanceof StateSnapshot.IncompatibleSchemaException) return error.getMessage();
        if (error instanceof StateClient.HttpStatusException) {
            return getString(R.string.http_status, ((StateClient.HttpStatusException) error).statusCode);
        }
        String name = error.getClass().getSimpleName();
        return name.isEmpty() ? getString(R.string.network_error) : name;
    }

    private String sourceStatusLabel(String status) {
        if ("ok".equals(status)) return getString(R.string.source_ok);
        if ("stale".equals(status)) return getString(R.string.source_stale);
        if ("error".equals(status)) return getString(R.string.source_error);
        if ("disabled".equals(status)) return getString(R.string.source_disabled);
        return getString(R.string.source_unknown);
    }

    private String taskStatusLabel(String status) {
        if ("running".equals(status)) return getString(R.string.task_running);
        if ("waiting".equals(status)) return getString(R.string.task_waiting);
        if ("failed".equals(status)) return getString(R.string.task_failed);
        if ("completed".equals(status)) return getString(R.string.task_completed);
        return getString(R.string.task_unknown);
    }

    private String valueOrDash(String value) {
        return value == null || value.isEmpty() ? getString(R.string.placeholder_dash) : value;
    }

    private String onOff(boolean enabled) {
        return getString(enabled ? R.string.setting_on : R.string.setting_off);
    }

    private static int taskPriority(String status) {
        if ("failed".equals(status)) return 0;
        if ("running".equals(status)) return 1;
        if ("waiting".equals(status)) return 2;
        if ("unknown".equals(status)) return 3;
        if ("completed".equals(status)) return 4;
        return 5;
    }

    private int taskColor(String status) {
        if ("failed".equals(status)) return color(R.color.hud_error);
        if ("running".equals(status)) return color(R.color.hud_success);
        if ("waiting".equals(status)) return color(R.color.hud_warning);
        return color(R.color.hud_text_secondary);
    }

    private int sourceHealthColor(String status) {
        if ("ok".equals(status)) return color(R.color.hud_success);
        if ("stale".equals(status)) return color(R.color.hud_warning);
        if ("error".equals(status)) return color(R.color.hud_error);
        return color(R.color.hud_text_secondary);
    }

    private int usageColor(double percent) {
        if (percent >= 90.0) return color(R.color.hud_error);
        if (percent >= 70.0) return color(R.color.hud_warning);
        return color(R.color.hud_accent);
    }

    private String formatDuration(int totalSeconds) {
        int safe = Math.max(0, totalSeconds);
        int hours = safe / 3600;
        int minutes = (safe % 3600) / 60;
        int seconds = safe % 60;
        if (hours > 0) return getString(R.string.duration_hours_minutes, hours, minutes);
        if (minutes > 0) return getString(R.string.duration_minutes_seconds, minutes, seconds);
        return getString(R.string.duration_seconds, seconds);
    }

    private String formatResetCountdown(String isoTimestamp) {
        Long resetMillis = IsoTime.parseMillis(isoTimestamp);
        if (resetMillis == null) return getString(R.string.placeholder_dash);
        long remainingSeconds = Math.max(0L, (resetMillis - System.currentTimeMillis()) / 1000L);
        long days = remainingSeconds / 86400L;
        long hours = (remainingSeconds % 86400L) / 3600L;
        long minutes = (remainingSeconds % 3600L) / 60L;
        long seconds = remainingSeconds % 60L;
        if (days > 0) return getString(R.string.duration_days_hours, days, hours);
        return getString(R.string.duration_clock, hours, minutes, seconds);
    }

    private static String join(List<String> values, String separator) {
        StringBuilder result = new StringBuilder();
        for (String value : values) {
            if (result.length() > 0) result.append(separator);
            result.append(value);
        }
        return result.toString();
    }

    private int color(int resourceId) {
        return getColor(resourceId);
    }

    private int dimenPx(int resourceId) {
        return getResources().getDimensionPixelSize(resourceId);
    }

    private void setTextSize(TextView view, int resourceId) {
        view.setTextSize(TypedValue.COMPLEX_UNIT_PX, getResources().getDimension(resourceId));
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
