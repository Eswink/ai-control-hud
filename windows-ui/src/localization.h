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
    ServiceStartAction,
    ServiceStopAction,
    ServiceRestartAction,
    ServiceUpgradeAction,
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

    // Source-compatible aliases for UI6 code written before the Win32
    // StartService macro collision was discovered. The UI does not call the
    // SCM StartService API directly; privileged mutations are CLI-delegated.
    StartService = ServiceStartAction,
    StopService = ServiceStopAction,
    RestartService = ServiceRestartAction,
    UpgradeService = ServiceUpgradeAction,
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
