#include "app.h"

#include <Windows.h>
#include <winrt/base.h>

#include <exception>

int WINAPI wWinMain(HINSTANCE instance, HINSTANCE, PWSTR, int showCommand) {
    (void)SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
    try {
        winrt::init_apartment(winrt::apartment_type::single_threaded);
        aicontrol::ui::App app;
        const int code = app.Run(instance, showCommand);
        winrt::uninit_apartment();
        return code;
    } catch (const winrt::hresult_error& error) {
        MessageBoxW(nullptr, error.message().c_str(), L"AI Control HUD — Agent UI", MB_OK | MB_ICONERROR);
    } catch (const std::exception&) {
        MessageBoxW(nullptr, L"The native Agent UI failed to initialize.", L"AI Control HUD — Agent UI", MB_OK | MB_ICONERROR);
    }
    return 1;
}
