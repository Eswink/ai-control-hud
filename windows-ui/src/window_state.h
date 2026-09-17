#pragma once

#include <Windows.h>

namespace aicontrol::ui {

struct WindowState {
    RECT normalRect{};
    bool hasNormalRect{false};
    bool miniHud{false};
};

[[nodiscard]] WindowState LoadWindowState() noexcept;
void SaveWindowState(const WindowState& state) noexcept;

}  // namespace aicontrol::ui
