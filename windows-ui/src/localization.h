#pragma once

#include <string_view>

namespace aicontrol::ui {

enum class TextId {
    AppTitle,
    AppSubtitle,
    Dashboard,
    Sources,
    Diagnostics,
    Settings,
    MiniHud,
    FullDashboard,
    AgentStatus,
    WindowsService,
    HubOutbox,
    AgentVersion,
    CurrentTask,
    NoCurrentTask,
    TaskList,
    UsageWindows,
    Workspace,
    ResetAt,
    OutboxBaselines,
    OutboxCompacted,
    LocalApi,
    NoData,
    ServiceActions,
    StartService,
    StopService,
    RestartService,
    UpgradeService,
    ConfirmPrivileged,
    PrivilegedLaunchFailed,
    CommandCode,
    ZCode,
    Plan,
    Remaining,
    Uptime,
    OutboxPending,
    Schema,
    Live,
    Degraded,
    Offline,
    ServerError,
    SchemaError,
    Running,
    Stopped,
    NotInstalled,
    Unknown,
    SourceOk,
    SourceStale,
    SourceError,
    SourceDisabled,
    OpenDashboard,
    Exit,
    LocalApiUnavailable,
};

class Localization final {
public:
    Localization();
    explicit Localization(bool simplifiedChinese) noexcept;

    [[nodiscard]] std::wstring_view Get(TextId id) const noexcept;
    [[nodiscard]] bool IsSimplifiedChinese() const noexcept { return simplifiedChinese_; }

private:
    bool simplifiedChinese_{false};
};

}  // namespace aicontrol::ui
