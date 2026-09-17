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

}  // namespace aicontrol::ui
