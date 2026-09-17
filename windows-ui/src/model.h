#pragma once

#include <algorithm>
#include <cstdint>
#include <optional>
#include <string>
#include <vector>

namespace aicontrol::ui {

enum class ConnectionState {
    Connecting,
    Live,
    Degraded,
    Offline,
    ServerError,
    SchemaError,
};

enum class ServiceState {
    Unknown,
    NotInstalled,
    Stopped,
    StartPending,
    StopPending,
    Running,
    Paused,
};

struct TaskView {
    std::wstring title;
    std::wstring workspace;
    std::wstring status;
    std::wstring activity;
    std::optional<std::int64_t> durationSeconds;
};

struct UsageWindowView {
    std::wstring name;
    std::optional<double> usedPercent;
    std::wstring resetAt;
};

struct OutboxView {
    bool available{false};
    std::wstring status;
    std::int64_t pendingEvents{0};
    std::int64_t taskBaselineRows{0};
    std::int64_t compactedTaskRows{0};
};

struct DashboardSnapshot {
    ConnectionState connection{ConnectionState::Connecting};
    ServiceState service{ServiceState::Unknown};
    std::wstring error;
    std::wstring overallStatus;
    std::wstring serverVersion;
    std::wstring agentVersion;
    std::wstring zcodeStatus;
    std::wstring commandCodeStatus;
    std::wstring plan;
    std::optional<double> creditRemaining;
    std::optional<double> creditLimit;
    std::wstring creditUnit;
    std::vector<UsageWindowView> usageWindows;
    std::vector<TaskView> tasks;
    std::optional<std::int64_t> uptimeSeconds;
    OutboxView outbox;
};

inline int TaskPriority(const std::wstring& status) noexcept {
    if (status == L"running") return 0;
    if (status == L"waiting") return 1;
    if (status == L"failed") return 2;
    if (status == L"unknown") return 3;
    if (status == L"completed") return 4;
    return 5;
}

inline const TaskView* CurrentTask(const DashboardSnapshot& snapshot) noexcept {
    if (snapshot.tasks.empty()) return nullptr;
    const TaskView* best = &snapshot.tasks.front();
    for (const auto& task : snapshot.tasks) {
        if (TaskPriority(task.status) < TaskPriority(best->status)) best = &task;
    }
    return best;
}

inline std::optional<double> CreditRemainingPercent(const DashboardSnapshot& snapshot) noexcept {
    if (!snapshot.creditRemaining || !snapshot.creditLimit || *snapshot.creditLimit <= 0.0) {
        return std::nullopt;
    }
    const double value = (*snapshot.creditRemaining / *snapshot.creditLimit) * 100.0;
    return std::clamp(value, 0.0, 100.0);
}

inline bool DisplayEquivalent(const DashboardSnapshot& left, const DashboardSnapshot& right) noexcept {
    if (left.connection != right.connection || left.service != right.service || left.error != right.error ||
        left.overallStatus != right.overallStatus || left.serverVersion != right.serverVersion ||
        left.agentVersion != right.agentVersion || left.zcodeStatus != right.zcodeStatus ||
        left.commandCodeStatus != right.commandCodeStatus || left.plan != right.plan ||
        left.creditRemaining != right.creditRemaining || left.creditLimit != right.creditLimit ||
        left.creditUnit != right.creditUnit || left.usageWindows.size() != right.usageWindows.size() ||
        left.tasks.size() != right.tasks.size() || left.outbox.available != right.outbox.available ||
        left.outbox.status != right.outbox.status || left.outbox.pendingEvents != right.outbox.pendingEvents ||
        left.outbox.taskBaselineRows != right.outbox.taskBaselineRows ||
        left.outbox.compactedTaskRows != right.outbox.compactedTaskRows) {
        return false;
    }

    for (std::size_t i = 0; i < left.usageWindows.size(); ++i) {
        const auto& a = left.usageWindows[i];
        const auto& b = right.usageWindows[i];
        if (a.name != b.name || a.usedPercent != b.usedPercent || a.resetAt != b.resetAt) return false;
    }
    for (std::size_t i = 0; i < left.tasks.size(); ++i) {
        const auto& a = left.tasks[i];
        const auto& b = right.tasks[i];
        if (a.title != b.title || a.workspace != b.workspace || a.status != b.status || a.activity != b.activity) {
            return false;
        }
    }

    // uptimeSeconds and per-task durationSeconds advance with wall time. Keeping the latest values in memory
    // but excluding them from repaint equivalence prevents a fixed 2-second paint loop.
    return true;
}

}  // namespace aicontrol::ui
