#pragma once

#include <Windows.h>

namespace aicontrol::ui {

enum class PrivilegedAction {
    Start,
    Stop,
    Restart,
    Upgrade,
};

struct PrivilegedLaunchResult {
    bool launched{false};
    DWORD error{ERROR_SUCCESS};
};

[[nodiscard]] const wchar_t* PrivilegedActionName(PrivilegedAction action) noexcept;
[[nodiscard]] PrivilegedLaunchResult LaunchPrivilegedServiceAction(PrivilegedAction action, HWND owner) noexcept;

}  // namespace aicontrol::ui
