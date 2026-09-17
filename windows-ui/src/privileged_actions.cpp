#include "privileged_actions.h"

#include <commdlg.h>
#include <shellapi.h>

#include <array>
#include <cwchar>
#include <string>

namespace aicontrol::ui {
namespace {

std::wstring InstalledAgentPath() noexcept {
    const DWORD required = GetEnvironmentVariableW(L"ProgramFiles", nullptr, 0);
    if (required == 0) return {};
    std::wstring root(static_cast<std::size_t>(required), L'\0');
    const DWORD written = GetEnvironmentVariableW(L"ProgramFiles", root.data(), required);
    if (written == 0 || written >= required) return {};
    root.resize(static_cast<std::size_t>(written));
    if (!root.empty() && root.back() != L'\\' && root.back() != L'/') root.push_back(L'\\');
    root += L"AI Control HUD\\ai-control-agent.exe";
    return root;
}

bool IsRegularFile(const std::wstring& path) noexcept {
    if (path.empty()) return false;
    const DWORD attributes = GetFileAttributesW(path.c_str());
    return attributes != INVALID_FILE_ATTRIBUTES && (attributes & FILE_ATTRIBUTE_DIRECTORY) == 0;
}

std::wstring SelectUpgradeCandidate(HWND owner) noexcept {
    std::array<wchar_t, 32768> path{};
    static constexpr wchar_t filter[] =
        L"AI Control Agent (ai-control-agent*.exe)\0ai-control-agent*.exe\0"
        L"Windows executable (*.exe)\0*.exe\0"
        L"All files (*.*)\0*.*\0\0";

    OPENFILENAMEW dialog{};
    dialog.lStructSize = sizeof(dialog);
    dialog.hwndOwner = owner;
    dialog.lpstrFilter = filter;
    dialog.lpstrFile = path.data();
    dialog.nMaxFile = static_cast<DWORD>(path.size());
    dialog.lpstrTitle = L"Select the new AI Control Agent binary";
    dialog.Flags = OFN_FILEMUSTEXIST | OFN_PATHMUSTEXIST | OFN_DONTADDTORECENT | OFN_NOCHANGEDIR;
    if (GetOpenFileNameW(&dialog) == FALSE) return {};
    return std::wstring(path.data());
}

}  // namespace

const wchar_t* PrivilegedActionName(PrivilegedAction action) noexcept {
    switch (action) {
        case PrivilegedAction::Start: return L"start";
        case PrivilegedAction::Stop: return L"stop";
        case PrivilegedAction::Restart: return L"restart";
        case PrivilegedAction::Upgrade: return L"upgrade";
    }
    return L"";
}

PrivilegedLaunchResult LaunchPrivilegedServiceAction(PrivilegedAction action, HWND owner) noexcept {
    const wchar_t* verb = PrivilegedActionName(action);
    if (verb[0] == L'\0') return {false, ERROR_INVALID_PARAMETER};

    const std::wstring installed = InstalledAgentPath();
    if (!IsRegularFile(installed)) return {false, ERROR_FILE_NOT_FOUND};

    std::wstring executable = installed;
    if (action == PrivilegedAction::Upgrade) {
        executable = SelectUpgradeCandidate(owner);
        if (executable.empty()) return {false, ERROR_CANCELLED};
        if (!IsRegularFile(executable)) return {false, ERROR_FILE_NOT_FOUND};
        if (_wcsicmp(executable.c_str(), installed.c_str()) == 0) {
            return {false, ERROR_ALREADY_EXISTS};
        }
    }

    const std::wstring parameters = std::wstring(L"service ") + verb;
    SHELLEXECUTEINFOW info{};
    info.cbSize = sizeof(info);
    info.fMask = SEE_MASK_NOCLOSEPROCESS | SEE_MASK_FLAG_NO_UI;
    info.hwnd = owner;
    info.lpVerb = L"runas";
    info.lpFile = executable.c_str();
    info.lpParameters = parameters.c_str();
    info.nShow = SW_HIDE;

    if (ShellExecuteExW(&info) == FALSE) return {false, GetLastError()};
    if (info.hProcess != nullptr) CloseHandle(info.hProcess);
    return {true, ERROR_SUCCESS};
}

}  // namespace aicontrol::ui
