#pragma once

#include "localization.h"
#include "model.h"

#include <Windows.h>
#include <d2d1.h>
#include <dwrite.h>
#include <wrl/client.h>

#include <atomic>
#include <condition_variable>
#include <mutex>
#include <thread>

namespace aicontrol::ui {

class App final {
public:
    App();
    ~App();

    App(const App&) = delete;
    App& operator=(const App&) = delete;

    int Run(HINSTANCE instance, int showCommand);

private:
    enum class Page {
        Dashboard,
        Sources,
        Diagnostics,
    };

    static constexpr UINT kSnapshotMessage = WM_APP + 1;
    static constexpr UINT kTrayMessage = WM_APP + 2;
    static constexpr UINT kTrayOpen = 41001;
    static constexpr UINT kTrayExit = 41002;

    static LRESULT CALLBACK WindowProc(HWND window, UINT message, WPARAM wParam, LPARAM lParam);
    LRESULT HandleMessage(UINT message, WPARAM wParam, LPARAM lParam);

    bool RegisterWindowClass();
    bool CreateMainWindow(int showCommand);
    bool CreateDeviceIndependentResources();
    bool EnsureRenderTarget();
    void DiscardRenderTarget();
    void Draw();
    void Resize();

    void AddTrayIcon();
    void RemoveTrayIcon();
    void ShowTrayMenu(POINT point);
    void ShowDashboard();
    void HideDashboard();

    void StartPoller();
    void StopPoller();
    DashboardSnapshot SnapshotCopy() const;

    void DrawCard(ID2D1RenderTarget* target, const D2D1_RECT_F& rect, ID2D1Brush* fill, float radius = 14.0f);
    void DrawTextBlock(
        ID2D1RenderTarget* target,
        std::wstring_view text,
        IDWriteTextFormat* format,
        const D2D1_RECT_F& rect,
        ID2D1Brush* brush,
        DWRITE_TEXT_ALIGNMENT alignment = DWRITE_TEXT_ALIGNMENT_LEADING
    );

    HINSTANCE instance_{nullptr};
    HWND window_{nullptr};
    NOTIFYICONDATAW tray_{};
    bool trayAdded_{false};
    bool exitRequested_{false};
    Page page_{Page::Dashboard};

    Localization localization_;

    mutable std::mutex snapshotMutex_;
    DashboardSnapshot snapshot_;
    std::jthread poller_;
    std::atomic_bool visible_{true};
    std::condition_variable_any pollWake_;
    std::mutex pollWakeMutex_;

    Microsoft::WRL::ComPtr<ID2D1Factory> d2dFactory_;
    Microsoft::WRL::ComPtr<IDWriteFactory> dwriteFactory_;
    Microsoft::WRL::ComPtr<ID2D1HwndRenderTarget> renderTarget_;

    Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> backgroundBrush_;
    Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> sidebarBrush_;
    Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> surfaceBrush_;
    Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> surfaceStrongBrush_;
    Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> borderBrush_;
    Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> primaryBrush_;
    Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> secondaryBrush_;
    Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> accentBrush_;
    Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> successBrush_;
    Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> warningBrush_;
    Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> errorBrush_;

    Microsoft::WRL::ComPtr<IDWriteTextFormat> titleFormat_;
    Microsoft::WRL::ComPtr<IDWriteTextFormat> sectionFormat_;
    Microsoft::WRL::ComPtr<IDWriteTextFormat> heroFormat_;
    Microsoft::WRL::ComPtr<IDWriteTextFormat> bodyFormat_;
    Microsoft::WRL::ComPtr<IDWriteTextFormat> smallFormat_;
};

}  // namespace aicontrol::ui
