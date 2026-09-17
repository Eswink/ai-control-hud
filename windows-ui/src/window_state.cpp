#include "window_state.h"

#include <filesystem>
#include <fstream>
#include <string>
#include <system_error>

namespace aicontrol::ui {
namespace {

std::filesystem::path StatePath() noexcept {
    const DWORD required = GetEnvironmentVariableW(L"LOCALAPPDATA", nullptr, 0);
    if (required == 0) return {};
    std::wstring root(static_cast<std::size_t>(required), L'\0');
    const DWORD written = GetEnvironmentVariableW(L"LOCALAPPDATA", root.data(), required);
    if (written == 0 || written >= required) return {};
    root.resize(static_cast<std::size_t>(written));
    return std::filesystem::path(root) / L"AIControlHUD" / L"native-ui-state.txt";
}

bool ValidRect(const RECT& rect) noexcept {
    const long width = rect.right - rect.left;
    const long height = rect.bottom - rect.top;
    return width >= 320 && width <= 10000 && height >= 160 && height <= 10000 &&
           rect.left >= -100000 && rect.left <= 100000 && rect.top >= -100000 && rect.top <= 100000;
}

}  // namespace

WindowState LoadWindowState() noexcept {
    WindowState state;
    try {
        const auto path = StatePath();
        if (path.empty()) return state;
        std::wifstream input(path);
        long left = 0;
        long top = 0;
        long width = 0;
        long height = 0;
        int mini = 0;
        if (!(input >> left >> top >> width >> height >> mini)) return state;
        RECT rect{left, top, left + width, top + height};
        if (!ValidRect(rect)) return state;
        state.normalRect = rect;
        state.hasNormalRect = true;
        state.miniHud = mini == 1;
    } catch (...) {
        return WindowState{};
    }
    return state;
}

void SaveWindowState(const WindowState& state) noexcept {
    if (!state.hasNormalRect || !ValidRect(state.normalRect)) return;
    try {
        const auto path = StatePath();
        if (path.empty()) return;
        std::error_code error;
        std::filesystem::create_directories(path.parent_path(), error);
        if (error) return;

        const auto temporary = path.wstring() + L".tmp";
        {
            std::wofstream output(temporary, std::ios::trunc);
            if (!output) return;
            output << state.normalRect.left << L' ' << state.normalRect.top << L' '
                   << (state.normalRect.right - state.normalRect.left) << L' '
                   << (state.normalRect.bottom - state.normalRect.top) << L' '
                   << (state.miniHud ? 1 : 0) << L'\n';
            output.flush();
            if (!output) return;
        }

        std::filesystem::rename(temporary, path, error);
        if (error) {
            error.clear();
            std::filesystem::remove(path, error);
            error.clear();
            std::filesystem::rename(temporary, path, error);
        }
        if (error) {
            error.clear();
            std::filesystem::remove(temporary, error);
        }
    } catch (...) {
        return;
    }
}

}  // namespace aicontrol::ui
