#include "localization.h"

#include <Windows.h>

#include <cwchar>

namespace aicontrol::ui {
namespace {

bool SystemUsesSimplifiedChinese() noexcept {
    wchar_t localeName[LOCALE_NAME_MAX_LENGTH]{};
    if (GetUserDefaultLocaleName(localeName, LOCALE_NAME_MAX_LENGTH) == 0) return false;
    return _wcsnicmp(localeName, L"zh-CN", 5) == 0 ||
           _wcsnicmp(localeName, L"zh-SG", 5) == 0 ||
           _wcsnicmp(localeName, L"zh-Hans", 7) == 0;
}

std::wstring_view En(TextId id) noexcept {
    switch (id) {
        case TextId::AppTitle: return L"AI Control HUD — Agent";
        case TextId::AppSubtitle: return L"Local Agent dashboard · native Windows UI";
        case TextId::Dashboard: return L"Dashboard";
        case TextId::Sources: return L"Sources";
        case TextId::Diagnostics: return L"Diagnostics";
        case TextId::Settings: return L"Settings";
        case TextId::AgentStatus: return L"Agent status";
        case TextId::WindowsService: return L"Windows service";
        case TextId::HubOutbox: return L"Hub outbox";
        case TextId::AgentVersion: return L"Agent version";
        case TextId::CurrentTask: return L"Current task";
        case TextId::NoCurrentTask: return L"No running or waiting task";
        case TextId::TaskList: return L"Task list";
        case TextId::UsageWindows: return L"Usage windows";
        case TextId::Workspace: return L"Workspace";
        case TextId::ResetAt: return L"Reset";
        case TextId::OutboxBaselines: return L"Task baseline rows";
        case TextId::OutboxCompacted: return L"Compacted task rows";
        case TextId::LocalApi: return L"Local API";
        case TextId::NoData: return L"No data available";
        case TextId::CommandCode: return L"CommandCode";
        case TextId::ZCode: return L"ZCode";
        case TextId::Plan: return L"Plan";
        case TextId::Remaining: return L"Remaining";
        case TextId::Uptime: return L"Uptime";
        case TextId::OutboxPending: return L"Pending events";
        case TextId::Schema: return L"Schema";
        case TextId::Live: return L"Live";
        case TextId::Degraded: return L"Degraded";
        case TextId::Offline: return L"Offline";
        case TextId::ServerError: return L"Server error";
        case TextId::SchemaError: return L"Schema error";
        case TextId::Running: return L"Running";
        case TextId::Stopped: return L"Stopped";
        case TextId::NotInstalled: return L"Not installed";
        case TextId::Unknown: return L"Unknown";
        case TextId::SourceOk: return L"Healthy";
        case TextId::SourceStale: return L"Stale";
        case TextId::SourceError: return L"Unavailable";
        case TextId::SourceDisabled: return L"Disabled";
        case TextId::OpenDashboard: return L"Open dashboard";
        case TextId::Exit: return L"Exit UI";
        case TextId::LocalApiUnavailable: return L"Local Agent API unavailable";
    }
    return L"—";
}

std::wstring_view Zh(TextId id) noexcept {
    switch (id) {
        case TextId::AppTitle: return L"AI Control HUD — Agent";
        case TextId::AppSubtitle: return L"本机 Agent 控制台 · Windows 原生界面";
        case TextId::Dashboard: return L"仪表盘";
        case TextId::Sources: return L"数据源";
        case TextId::Diagnostics: return L"诊断信息";
        case TextId::Settings: return L"设置";
        case TextId::AgentStatus: return L"Agent 状态";
        case TextId::WindowsService: return L"Windows 服务";
        case TextId::HubOutbox: return L"Hub 上传队列";
        case TextId::AgentVersion: return L"Agent 版本";
        case TextId::CurrentTask: return L"当前任务";
        case TextId::NoCurrentTask: return L"当前没有运行或等待中的任务";
        case TextId::TaskList: return L"任务列表";
        case TextId::UsageWindows: return L"额度窗口";
        case TextId::Workspace: return L"工作区";
        case TextId::ResetAt: return L"重置时间";
        case TextId::OutboxBaselines: return L"任务基线行";
        case TextId::OutboxCompacted: return L"已压缩任务行";
        case TextId::LocalApi: return L"本机 API";
        case TextId::NoData: return L"暂无可用数据";
        case TextId::CommandCode: return L"CommandCode";
        case TextId::ZCode: return L"ZCode";
        case TextId::Plan: return L"套餐";
        case TextId::Remaining: return L"剩余";
        case TextId::Uptime: return L"运行时间";
        case TextId::OutboxPending: return L"待上传事件";
        case TextId::Schema: return L"数据结构版本";
        case TextId::Live: return L"运行中";
        case TextId::Degraded: return L"状态降级";
        case TextId::Offline: return L"离线";
        case TextId::ServerError: return L"服务错误";
        case TextId::SchemaError: return L"版本不兼容";
        case TextId::Running: return L"运行中";
        case TextId::Stopped: return L"已停止";
        case TextId::NotInstalled: return L"未安装";
        case TextId::Unknown: return L"未知";
        case TextId::SourceOk: return L"健康";
        case TextId::SourceStale: return L"数据过期";
        case TextId::SourceError: return L"数据不可用";
        case TextId::SourceDisabled: return L"未启用";
        case TextId::OpenDashboard: return L"打开仪表盘";
        case TextId::Exit: return L"退出界面";
        case TextId::LocalApiUnavailable: return L"本机 Agent API 不可用";
    }
    return L"—";
}

}  // namespace

Localization::Localization() : simplifiedChinese_(SystemUsesSimplifiedChinese()) {}

Localization::Localization(bool simplifiedChinese) noexcept : simplifiedChinese_(simplifiedChinese) {}

std::wstring_view Localization::Get(TextId id) const noexcept {
    return simplifiedChinese_ ? Zh(id) : En(id);
}

}  // namespace aicontrol::ui
