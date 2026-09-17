#include "app.h"

#include "agent_client.h"
#include "health_probe.h"

#include <dwmapi.h>
#include <shellapi.h>
#include <winrt/base.h>

#include <algorithm>
#include <chrono>
#include <cmath>
#include <iomanip>
#include <sstream>
#include <string>

namespace aicontrol::ui {
namespace {

constexpr wchar_t kWindowClass[] = L"AIControlHUD.AgentUI.Window";
constexpr UINT kTrayId = 1;
constexpr float kSidebarWidth = 214.0f;
constexpr float kOuterGap = 24.0f;
constexpr float kCardGap = 16.0f;

D2D1_COLOR_F Rgb(UINT32 rgb, float alpha = 1.0f) noexcept {
    return D2D1::ColorF(rgb, alpha);
}

std::wstring ToFixed(double value, int precision = 1) {
    std::wostringstream out;
    out << std::fixed << std::setprecision(precision) << value;
    return out.str();
}

std::wstring DurationText(std::int64_t seconds) {
    seconds = std::max<std::int64_t>(0, seconds);
    const auto hours = seconds / 3600;
    const auto minutes = (seconds % 3600) / 60;
    if (hours > 0) return std::to_wstring(hours) + L"h " + std::to_wstring(minutes) + L"m";
    return std::to_wstring(minutes) + L"m";
}

std::wstring ConnectionText(const Localization& locale, ConnectionState state) {
    switch (state) {
        case ConnectionState::Live: return std::wstring(locale.Get(TextId::Live));
        case ConnectionState::Degraded: return std::wstring(locale.Get(TextId::Degraded));
        case ConnectionState::Offline: return std::wstring(locale.Get(TextId::Offline));
        case ConnectionState::ServerError: return std::wstring(locale.Get(TextId::ServerError));
        case ConnectionState::SchemaError: return std::wstring(locale.Get(TextId::SchemaError));
        case ConnectionState::Connecting: return L"…";
    }
    return L"—";
}

std::wstring ServiceText(const Localization& locale, ServiceState state) {
    switch (state) {
        case ServiceState::Running: return std::wstring(locale.Get(TextId::Running));
        case ServiceState::Stopped: return std::wstring(locale.Get(TextId::Stopped));
        case ServiceState::NotInstalled: return std::wstring(locale.Get(TextId::NotInstalled));
        case ServiceState::StartPending: return L"STARTING";
        case ServiceState::StopPending: return L"STOPPING";
        case ServiceState::Paused: return L"PAUSED";
        case ServiceState::Unknown: return std::wstring(locale.Get(TextId::Unknown));
    }
    return L"—";
}

std::wstring SourceText(const Localization& locale, const std::wstring& value) {
    if (value == L"ok") return std::wstring(locale.Get(TextId::SourceOk));
    if (value == L"stale") return std::wstring(locale.Get(TextId::SourceStale));
    if (value == L"error") return std::wstring(locale.Get(TextId::SourceError));
    if (value == L"disabled") return std::wstring(locale.Get(TextId::SourceDisabled));
    return std::wstring(locale.Get(TextId::Unknown));
}

}  // namespace

App::App() = default;

App::~App() {
    StopPoller();
    RemoveTrayIcon();
    DiscardRenderTarget();
}

int App::Run(HINSTANCE instance, int showCommand) {
    instance_ = instance;
    if (!CreateDeviceIndependentResources() || !RegisterWindowClass() || !CreateMainWindow(showCommand)) {
        return 1;
    }

    AddTrayIcon();
    StartPoller();

    MSG message{};
    while (GetMessageW(&message, nullptr, 0, 0) > 0) {
        TranslateMessage(&message);
        DispatchMessageW(&message);
    }
    return static_cast<int>(message.wParam);
}

bool App::RegisterWindowClass() {
    WNDCLASSEXW windowClass{};
    windowClass.cbSize = sizeof(windowClass);
    windowClass.style = CS_HREDRAW | CS_VREDRAW;
    windowClass.lpfnWndProc = &App::WindowProc;
    windowClass.hInstance = instance_;
    windowClass.hCursor = LoadCursorW(nullptr, IDC_ARROW);
    windowClass.hIcon = LoadIconW(nullptr, IDI_APPLICATION);
    windowClass.hIconSm = windowClass.hIcon;
    windowClass.lpszClassName = kWindowClass;
    if (RegisterClassExW(&windowClass) != 0) return true;
    return GetLastError() == ERROR_CLASS_ALREADY_EXISTS;
}

bool App::CreateMainWindow(int showCommand) {
    const std::wstring title(localization_.Get(TextId::AppTitle));
    window_ = CreateWindowExW(
        0,
        kWindowClass,
        title.c_str(),
        WS_OVERLAPPEDWINDOW,
        CW_USEDEFAULT,
        CW_USEDEFAULT,
        1120,
        720,
        nullptr,
        nullptr,
        instance_,
        this
    );
    if (window_ == nullptr) return false;

    BOOL dark = TRUE;
    constexpr DWORD kImmersiveDarkMode = 20;
    (void)DwmSetWindowAttribute(window_, kImmersiveDarkMode, &dark, sizeof(dark));

    const int command = showCommand == SW_HIDE ? SW_SHOWNORMAL : showCommand;
    ShowWindow(window_, command);
    UpdateWindow(window_);
    visible_.store(IsWindowVisible(window_) != FALSE, std::memory_order_relaxed);
    return true;
}

bool App::CreateDeviceIndependentResources() {
    HRESULT hr = D2D1CreateFactory(D2D1_FACTORY_TYPE_SINGLE_THREADED, d2dFactory_.GetAddressOf());
    if (FAILED(hr)) return false;

    hr = DWriteCreateFactory(
        DWRITE_FACTORY_TYPE_SHARED,
        __uuidof(IDWriteFactory),
        reinterpret_cast<IUnknown**>(dwriteFactory_.GetAddressOf())
    );
    if (FAILED(hr)) return false;

    const wchar_t* localeName = localization_.IsSimplifiedChinese() ? L"zh-CN" : L"en-US";
    const auto createFormat = [&](float size, DWRITE_FONT_WEIGHT weight, auto& target) -> bool {
        return SUCCEEDED(dwriteFactory_->CreateTextFormat(
            L"Segoe UI",
            nullptr,
            weight,
            DWRITE_FONT_STYLE_NORMAL,
            DWRITE_FONT_STRETCH_NORMAL,
            size,
            localeName,
            target.GetAddressOf()
        ));
    };

    return createFormat(28.0f, DWRITE_FONT_WEIGHT_SEMI_BOLD, titleFormat_) &&
           createFormat(17.0f, DWRITE_FONT_WEIGHT_SEMI_BOLD, sectionFormat_) &&
           createFormat(31.0f, DWRITE_FONT_WEIGHT_BOLD, heroFormat_) &&
           createFormat(14.0f, DWRITE_FONT_WEIGHT_NORMAL, bodyFormat_) &&
           createFormat(12.0f, DWRITE_FONT_WEIGHT_NORMAL, smallFormat_);
}

bool App::EnsureRenderTarget() {
    if (renderTarget_) return true;
    if (window_ == nullptr) return false;

    RECT client{};
    GetClientRect(window_, &client);
    const auto size = D2D1::SizeU(
        static_cast<UINT32>(std::max<LONG>(1, client.right - client.left)),
        static_cast<UINT32>(std::max<LONG>(1, client.bottom - client.top))
    );
    HRESULT hr = d2dFactory_->CreateHwndRenderTarget(
        D2D1::RenderTargetProperties(),
        D2D1::HwndRenderTargetProperties(window_, size),
        renderTarget_.GetAddressOf()
    );
    if (FAILED(hr)) return false;

    const auto brush = [&](UINT32 rgb, auto& target) -> bool {
        return SUCCEEDED(renderTarget_->CreateSolidColorBrush(Rgb(rgb), target.GetAddressOf()));
    };

    if (!brush(0x0B0F14, backgroundBrush_) ||
        !brush(0x101720, sidebarBrush_) ||
        !brush(0x151D27, surfaceBrush_) ||
        !brush(0x182432, surfaceStrongBrush_) ||
        !brush(0x293544, borderBrush_) ||
        !brush(0xF2F7FB, primaryBrush_) ||
        !brush(0x91A2B4, secondaryBrush_) ||
        !brush(0x52B8FF, accentBrush_) ||
        !brush(0x52D3A1, successBrush_) ||
        !brush(0xF4C75D, warningBrush_) ||
        !brush(0xFF6B78, errorBrush_)) {
        DiscardRenderTarget();
        return false;
    }
    return true;
}

void App::DiscardRenderTarget() {
    backgroundBrush_.Reset();
    sidebarBrush_.Reset();
    surfaceBrush_.Reset();
    surfaceStrongBrush_.Reset();
    borderBrush_.Reset();
    primaryBrush_.Reset();
    secondaryBrush_.Reset();
    accentBrush_.Reset();
    successBrush_.Reset();
    warningBrush_.Reset();
    errorBrush_.Reset();
    renderTarget_.Reset();
}

void App::DrawCard(ID2D1RenderTarget* target, const D2D1_RECT_F& rect, ID2D1Brush* fill, float radius) {
    const auto rounded = D2D1::RoundedRect(rect, radius, radius);
    target->FillRoundedRectangle(rounded, fill);
    target->DrawRoundedRectangle(rounded, borderBrush_.Get(), 1.0f);
}

void App::DrawTextBlock(
    ID2D1RenderTarget* target,
    std::wstring_view text,
    IDWriteTextFormat* format,
    const D2D1_RECT_F& rect,
    ID2D1Brush* brush,
    DWRITE_TEXT_ALIGNMENT alignment
) {
    if (text.empty() || format == nullptr || brush == nullptr) return;
    format->SetTextAlignment(alignment);
    target->DrawTextW(
        text.data(),
        static_cast<UINT32>(text.size()),
        format,
        rect,
        brush,
        D2D1_DRAW_TEXT_OPTIONS_CLIP
    );
    format->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
}

void App::Draw() {
    PAINTSTRUCT paint{};
    BeginPaint(window_, &paint);
    if (!EnsureRenderTarget()) {
        EndPaint(window_, &paint);
        return;
    }

    const DashboardSnapshot snapshot = SnapshotCopy();
    const D2D1_SIZE_F size = renderTarget_->GetSize();
    renderTarget_->BeginDraw();
    renderTarget_->Clear(Rgb(0x0B0F14));

    renderTarget_->FillRectangle(D2D1::RectF(0, 0, kSidebarWidth, size.height), sidebarBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::AppTitle), sectionFormat_.Get(),
                  D2D1::RectF(22, 26, kSidebarWidth - 18, 62), primaryBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::AppSubtitle), smallFormat_.Get(),
                  D2D1::RectF(22, 66, kSidebarWidth - 18, 114), secondaryBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::Dashboard), bodyFormat_.Get(),
                  D2D1::RectF(24, 145, kSidebarWidth - 20, 176), accentBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::Sources), bodyFormat_.Get(),
                  D2D1::RectF(24, 184, kSidebarWidth - 20, 215), secondaryBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::Diagnostics), bodyFormat_.Get(),
                  D2D1::RectF(24, 223, kSidebarWidth - 20, 254), secondaryBrush_.Get());

    const float left = kSidebarWidth + kOuterGap;
    const float right = std::max(left + 520.0f, size.width - kOuterGap);
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::Dashboard), titleFormat_.Get(),
                  D2D1::RectF(left, 22, right - 220, 66), primaryBrush_.Get());

    const std::wstring connection = ConnectionText(localization_, snapshot.connection);
    ID2D1Brush* connectionBrush = secondaryBrush_.Get();
    if (snapshot.connection == ConnectionState::Live) connectionBrush = successBrush_.Get();
    else if (snapshot.connection == ConnectionState::Degraded || snapshot.connection == ConnectionState::Connecting) connectionBrush = warningBrush_.Get();
    else if (snapshot.connection == ConnectionState::Offline || snapshot.connection == ConnectionState::ServerError || snapshot.connection == ConnectionState::SchemaError) connectionBrush = errorBrush_.Get();
    DrawTextBlock(renderTarget_.Get(), connection, sectionFormat_.Get(),
                  D2D1::RectF(right - 210, 26, right, 58), connectionBrush, DWRITE_TEXT_ALIGNMENT_TRAILING);

    const float contentWidth = right - left;
    const float heroTop = 82.0f;
    const float heroHeight = std::min(292.0f, std::max(220.0f, size.height * 0.43f));
    const float columnWidth = (contentWidth - kCardGap) / 2.0f;
    const D2D1_RECT_F commandRect = D2D1::RectF(left, heroTop, left + columnWidth, heroTop + heroHeight);
    const D2D1_RECT_F taskRect = D2D1::RectF(left + columnWidth + kCardGap, heroTop, right, heroTop + heroHeight);
    DrawCard(renderTarget_.Get(), commandRect, surfaceStrongBrush_.Get());
    DrawCard(renderTarget_.Get(), taskRect, surfaceBrush_.Get());

    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::CommandCode), sectionFormat_.Get(),
                  D2D1::RectF(commandRect.left + 20, commandRect.top + 17, commandRect.right - 20, commandRect.top + 48), primaryBrush_.Get());
    const std::wstring plan = snapshot.plan.empty() ? L"—" : snapshot.plan;
    DrawTextBlock(renderTarget_.Get(), std::wstring(localization_.Get(TextId::Plan)) + L"  " + plan,
                  bodyFormat_.Get(), D2D1::RectF(commandRect.left + 20, commandRect.top + 58, commandRect.right - 20, commandRect.top + 86), accentBrush_.Get());

    std::wstring credit = L"—";
    if (snapshot.creditRemaining) {
        credit = ToFixed(*snapshot.creditRemaining, 2);
        if (!snapshot.creditUnit.empty()) credit += L" " + snapshot.creditUnit;
        if (snapshot.creditLimit) credit += L" / " + ToFixed(*snapshot.creditLimit, 2);
    }
    DrawTextBlock(renderTarget_.Get(), credit, heroFormat_.Get(),
                  D2D1::RectF(commandRect.left + 20, commandRect.top + 96, commandRect.right - 20, commandRect.top + 146), successBrush_.Get());

    float usageY = commandRect.top + 162.0f;
    const std::size_t usageCount = std::min<std::size_t>(2, snapshot.usageWindows.size());
    for (std::size_t i = 0; i < usageCount; ++i) {
        const auto& usage = snapshot.usageWindows[i];
        std::wstring line = usage.name + L"  ";
        line += usage.usedPercent ? ToFixed(*usage.usedPercent, 1) + L"%" : L"—";
        DrawTextBlock(renderTarget_.Get(), line, bodyFormat_.Get(),
                      D2D1::RectF(commandRect.left + 20, usageY, commandRect.right - 20, usageY + 26), secondaryBrush_.Get());
        if (usage.usedPercent) {
            const float fraction = static_cast<float>(std::clamp(*usage.usedPercent, 0.0, 100.0) / 100.0);
            const D2D1_RECT_F rail = D2D1::RectF(commandRect.left + 20, usageY + 29, commandRect.right - 20, usageY + 36);
            renderTarget_->FillRoundedRectangle(D2D1::RoundedRect(rail, 3, 3), borderBrush_.Get());
            const D2D1_RECT_F fill = D2D1::RectF(rail.left, rail.top, rail.left + (rail.right - rail.left) * fraction, rail.bottom);
            renderTarget_->FillRoundedRectangle(D2D1::RoundedRect(fill, 3, 3), accentBrush_.Get());
        }
        usageY += 54.0f;
    }

    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::CurrentTask), sectionFormat_.Get(),
                  D2D1::RectF(taskRect.left + 20, taskRect.top + 17, taskRect.right - 20, taskRect.top + 48), primaryBrush_.Get());
    const TaskView* current = CurrentTask(snapshot);
    if (current == nullptr) {
        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::NoCurrentTask), bodyFormat_.Get(),
                      D2D1::RectF(taskRect.left + 20, taskRect.top + 76, taskRect.right - 20, taskRect.top + 130), secondaryBrush_.Get());
    } else {
        DrawTextBlock(renderTarget_.Get(), current->title, heroFormat_.Get(),
                      D2D1::RectF(taskRect.left + 20, taskRect.top + 67, taskRect.right - 20, taskRect.top + 124), primaryBrush_.Get());
        std::wstring meta = current->status;
        if (!current->workspace.empty()) meta += L" · " + current->workspace;
        if (current->durationSeconds) meta += L" · " + DurationText(*current->durationSeconds);
        DrawTextBlock(renderTarget_.Get(), meta, bodyFormat_.Get(),
                      D2D1::RectF(taskRect.left + 20, taskRect.top + 132, taskRect.right - 20, taskRect.top + 161), accentBrush_.Get());
        if (!current->activity.empty()) {
            DrawTextBlock(renderTarget_.Get(), current->activity, bodyFormat_.Get(),
                          D2D1::RectF(taskRect.left + 20, taskRect.top + 178, taskRect.right - 20, taskRect.bottom - 20), secondaryBrush_.Get());
        }
    }

    const float lowerTop = heroTop + heroHeight + kCardGap;
    const float lowerBottom = std::max(lowerTop + 128.0f, size.height - kOuterGap);
    const D2D1_RECT_F sourceRect = D2D1::RectF(left, lowerTop, left + columnWidth, lowerBottom);
    const D2D1_RECT_F opsRect = D2D1::RectF(left + columnWidth + kCardGap, lowerTop, right, lowerBottom);
    DrawCard(renderTarget_.Get(), sourceRect, surfaceBrush_.Get());
    DrawCard(renderTarget_.Get(), opsRect, surfaceBrush_.Get());

    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::Sources), sectionFormat_.Get(),
                  D2D1::RectF(sourceRect.left + 20, sourceRect.top + 16, sourceRect.right - 20, sourceRect.top + 45), primaryBrush_.Get());
    const std::wstring zcode = std::wstring(localization_.Get(TextId::ZCode)) + L"  " + SourceText(localization_, snapshot.zcodeStatus);
    const std::wstring command = std::wstring(localization_.Get(TextId::CommandCode)) + L"  " + SourceText(localization_, snapshot.commandCodeStatus);
    DrawTextBlock(renderTarget_.Get(), zcode, bodyFormat_.Get(),
                  D2D1::RectF(sourceRect.left + 20, sourceRect.top + 58, sourceRect.right - 20, sourceRect.top + 85), secondaryBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), command, bodyFormat_.Get(),
                  D2D1::RectF(sourceRect.left + 20, sourceRect.top + 88, sourceRect.right - 20, sourceRect.top + 116), secondaryBrush_.Get());

    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::AgentStatus), sectionFormat_.Get(),
                  D2D1::RectF(opsRect.left + 20, opsRect.top + 16, opsRect.right - 20, opsRect.top + 45), primaryBrush_.Get());
    const std::wstring service = std::wstring(localization_.Get(TextId::WindowsService)) + L"  " + ServiceText(localization_, snapshot.service);
    DrawTextBlock(renderTarget_.Get(), service, bodyFormat_.Get(),
                  D2D1::RectF(opsRect.left + 20, opsRect.top + 58, opsRect.right - 20, opsRect.top + 85), secondaryBrush_.Get());
    std::wstring diagnostics = std::wstring(localization_.Get(TextId::AgentVersion)) + L"  " + (snapshot.agentVersion.empty() ? L"—" : snapshot.agentVersion);
    if (snapshot.uptimeSeconds) diagnostics += L"  ·  " + DurationText(*snapshot.uptimeSeconds);
    DrawTextBlock(renderTarget_.Get(), diagnostics, bodyFormat_.Get(),
                  D2D1::RectF(opsRect.left + 20, opsRect.top + 88, opsRect.right - 20, opsRect.top + 116), secondaryBrush_.Get());
    if (snapshot.outbox.available) {
        const std::wstring outbox = std::wstring(localization_.Get(TextId::OutboxPending)) + L"  " + std::to_wstring(snapshot.outbox.pendingEvents);
        DrawTextBlock(renderTarget_.Get(), outbox, smallFormat_.Get(),
                      D2D1::RectF(opsRect.left + 20, opsRect.top + 119, opsRect.right - 20, opsRect.bottom - 10), secondaryBrush_.Get());
    }

    const HRESULT hr = renderTarget_->EndDraw();
    if (hr == D2DERR_RECREATE_TARGET) DiscardRenderTarget();
    EndPaint(window_, &paint);
}

void App::Resize() {
    if (!renderTarget_ || window_ == nullptr) return;
    RECT client{};
    GetClientRect(window_, &client);
    renderTarget_->Resize(D2D1::SizeU(
        static_cast<UINT32>(std::max<LONG>(1, client.right - client.left)),
        static_cast<UINT32>(std::max<LONG>(1, client.bottom - client.top))
    ));
}

void App::AddTrayIcon() {
    if (trayAdded_ || window_ == nullptr) return;
    tray_ = {};
    tray_.cbSize = sizeof(tray_);
    tray_.hWnd = window_;
    tray_.uID = kTrayId;
    tray_.uFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP;
    tray_.uCallbackMessage = kTrayMessage;
    tray_.hIcon = LoadIconW(nullptr, IDI_APPLICATION);
    const std::wstring tip(localization_.Get(TextId::AppTitle));
    wcsncpy_s(tray_.szTip, tip.c_str(), _TRUNCATE);
    trayAdded_ = Shell_NotifyIconW(NIM_ADD, &tray_) != FALSE;
    if (trayAdded_) {
        tray_.uVersion = NOTIFYICON_VERSION_4;
        (void)Shell_NotifyIconW(NIM_SETVERSION, &tray_);
    }
}

void App::RemoveTrayIcon() {
    if (!trayAdded_) return;
    (void)Shell_NotifyIconW(NIM_DELETE, &tray_);
    trayAdded_ = false;
}

void App::ShowTrayMenu(POINT point) {
    HMENU menu = CreatePopupMenu();
    if (menu == nullptr) return;
    const std::wstring open(localization_.Get(TextId::OpenDashboard));
    const std::wstring exit(localization_.Get(TextId::Exit));
    AppendMenuW(menu, MF_STRING, kTrayOpen, open.c_str());
    AppendMenuW(menu, MF_SEPARATOR, 0, nullptr);
    AppendMenuW(menu, MF_STRING, kTrayExit, exit.c_str());
    SetForegroundWindow(window_);
    TrackPopupMenu(menu, TPM_RIGHTBUTTON | TPM_BOTTOMALIGN | TPM_LEFTALIGN, point.x, point.y, 0, window_, nullptr);
    DestroyMenu(menu);
}

void App::ShowDashboard() {
    if (window_ == nullptr) return;
    ShowWindow(window_, SW_RESTORE);
    SetForegroundWindow(window_);
    visible_.store(true, std::memory_order_relaxed);
    pollWake_.notify_all();
}

void App::HideDashboard() {
    if (window_ == nullptr) return;
    ShowWindow(window_, SW_HIDE);
    visible_.store(false, std::memory_order_relaxed);
    pollWake_.notify_all();
}

void App::StartPoller() {
    if (poller_.joinable()) return;
    poller_ = std::jthread([this](std::stop_token stop) {
        winrt::init_apartment(winrt::apartment_type::multi_threaded);
        try {
            AgentClient client;
            HealthProbe health;
            while (!stop.stop_requested()) {
                const bool visible = visible_.load(std::memory_order_relaxed);
                if (visible) {
                    DashboardSnapshot next = client.Poll();
                    {
                        std::lock_guard lock(snapshotMutex_);
                        snapshot_ = std::move(next);
                    }
                    if (window_ != nullptr) PostMessageW(window_, kSnapshotMessage, 0, 0);
                } else {
                    const bool healthy = health.Check();
                    const ServiceState service = QueryAgentServiceState();
                    std::lock_guard lock(snapshotMutex_);
                    snapshot_.service = service;
                    if (!healthy) snapshot_.connection = ConnectionState::Offline;
                }

                const auto delay = visible ? std::chrono::seconds(2) : std::chrono::seconds(20);
                std::unique_lock waitLock(pollWakeMutex_);
                pollWake_.wait_for(waitLock, delay);
            }
        } catch (...) {
            std::lock_guard lock(snapshotMutex_);
            snapshot_.connection = ConnectionState::Offline;
            snapshot_.error = L"poller initialization failed";
            if (window_ != nullptr) PostMessageW(window_, kSnapshotMessage, 0, 0);
        }
        winrt::uninit_apartment();
    });
}

void App::StopPoller() {
    if (!poller_.joinable()) return;
    poller_.request_stop();
    pollWake_.notify_all();
    poller_.join();
}

DashboardSnapshot App::SnapshotCopy() const {
    std::lock_guard lock(snapshotMutex_);
    return snapshot_;
}

LRESULT CALLBACK App::WindowProc(HWND window, UINT message, WPARAM wParam, LPARAM lParam) {
    App* app = nullptr;
    if (message == WM_NCCREATE) {
        const auto* create = reinterpret_cast<CREATESTRUCTW*>(lParam);
        app = static_cast<App*>(create->lpCreateParams);
        SetWindowLongPtrW(window, GWLP_USERDATA, reinterpret_cast<LONG_PTR>(app));
        app->window_ = window;
    } else {
        app = reinterpret_cast<App*>(GetWindowLongPtrW(window, GWLP_USERDATA));
    }
    return app != nullptr ? app->HandleMessage(message, wParam, lParam) : DefWindowProcW(window, message, wParam, lParam);
}

LRESULT App::HandleMessage(UINT message, WPARAM wParam, LPARAM lParam) {
    switch (message) {
        case WM_PAINT:
            Draw();
            return 0;
        case WM_ERASEBKGND:
            return 1;
        case WM_SIZE:
            Resize();
            visible_.store(wParam != SIZE_MINIMIZED && IsWindowVisible(window_) != FALSE, std::memory_order_relaxed);
            pollWake_.notify_all();
            return 0;
        case WM_SHOWWINDOW:
            visible_.store(wParam != FALSE, std::memory_order_relaxed);
            pollWake_.notify_all();
            return 0;
        case WM_DPICHANGED: {
            const auto* suggested = reinterpret_cast<RECT*>(lParam);
            SetWindowPos(window_, nullptr, suggested->left, suggested->top,
                         suggested->right - suggested->left, suggested->bottom - suggested->top,
                         SWP_NOZORDER | SWP_NOACTIVATE);
            return 0;
        }
        case WM_GETMINMAXINFO: {
            auto* info = reinterpret_cast<MINMAXINFO*>(lParam);
            info->ptMinTrackSize.x = 900;
            info->ptMinTrackSize.y = 580;
            return 0;
        }
        case kSnapshotMessage:
            if (IsWindowVisible(window_)) InvalidateRect(window_, nullptr, FALSE);
            return 0;
        case kTrayMessage:
            if (LOWORD(lParam) == WM_LBUTTONDBLCLK) {
                ShowDashboard();
            } else if (LOWORD(lParam) == WM_RBUTTONUP || LOWORD(lParam) == WM_CONTEXTMENU) {
                POINT point{};
                GetCursorPos(&point);
                ShowTrayMenu(point);
            }
            return 0;
        case WM_COMMAND:
            if (LOWORD(wParam) == kTrayOpen) {
                ShowDashboard();
                return 0;
            }
            if (LOWORD(wParam) == kTrayExit) {
                exitRequested_ = true;
                DestroyWindow(window_);
                return 0;
            }
            break;
        case WM_CLOSE:
            if (exitRequested_) DestroyWindow(window_);
            else HideDashboard();
            return 0;
        case WM_DESTROY:
            StopPoller();
            RemoveTrayIcon();
            window_ = nullptr;
            PostQuitMessage(0);
            return 0;
        default:
            break;
    }
    return DefWindowProcW(window_, message, wParam, lParam);
}

}  // namespace aicontrol::ui
