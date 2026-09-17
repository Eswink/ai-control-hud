#include "app.h"

#include "agent_client.h"

#include <dwmapi.h>
#include <shellapi.h>
#include <winrt/base.h>

#include <algorithm>
#include <chrono>
#include <condition_variable>
#include <iomanip>
#include <memory>
#include <sstream>
#include <string>
#include <thread>

namespace aicontrol::ui {
namespace {

constexpr wchar_t kWindowClass[] = L"AIControlHUD.AgentUI.Window";
constexpr wchar_t kWindowTitle[] = L"AI Control HUD — Agent";

constexpr D2D1_COLOR_F kBackground = {0.027f, 0.067f, 0.106f, 1.0f};
constexpr D2D1_COLOR_F kSidebar = {0.035f, 0.102f, 0.153f, 1.0f};
constexpr D2D1_COLOR_F kSurface = {0.051f, 0.129f, 0.192f, 1.0f};
constexpr D2D1_COLOR_F kSurfaceStrong = {0.063f, 0.165f, 0.239f, 1.0f};
constexpr D2D1_COLOR_F kBorder = {0.090f, 0.239f, 0.333f, 1.0f};
constexpr D2D1_COLOR_F kPrimary = {0.957f, 0.973f, 0.988f, 1.0f};
constexpr D2D1_COLOR_F kSecondary = {0.663f, 0.722f, 0.780f, 1.0f};
constexpr D2D1_COLOR_F kAccent = {0.153f, 0.725f, 1.0f, 1.0f};
constexpr D2D1_COLOR_F kSuccess = {0.180f, 0.902f, 0.651f, 1.0f};
constexpr D2D1_COLOR_F kWarning = {0.965f, 0.769f, 0.325f, 1.0f};
constexpr D2D1_COLOR_F kError = {1.0f, 0.420f, 0.447f, 1.0f};

std::wstring FormatNumber(double value) {
    std::wostringstream stream;
    stream << std::fixed << std::setprecision(value < 100.0 ? 2 : 0) << value;
    return stream.str();
}

std::wstring FormatDuration(std::int64_t totalSeconds) {
    const auto safe = std::max<std::int64_t>(0, totalSeconds);
    const auto hours = safe / 3600;
    const auto minutes = (safe % 3600) / 60;
    if (hours > 0) return std::to_wstring(hours) + L"h " + std::to_wstring(minutes) + L"m";
    const auto seconds = safe % 60;
    if (minutes > 0) return std::to_wstring(minutes) + L"m " + std::to_wstring(seconds) + L"s";
    return std::to_wstring(seconds) + L"s";
}

std::wstring ConnectionText(const Localization& strings, ConnectionState state) {
    switch (state) {
        case ConnectionState::Live: return std::wstring(strings.Get(TextId::Live));
        case ConnectionState::Degraded: return std::wstring(strings.Get(TextId::Degraded));
        case ConnectionState::Offline: return std::wstring(strings.Get(TextId::Offline));
        case ConnectionState::ServerError: return std::wstring(strings.Get(TextId::ServerError));
        case ConnectionState::SchemaError: return std::wstring(strings.Get(TextId::SchemaError));
        case ConnectionState::Connecting: return L"Connecting";
    }
    return std::wstring(strings.Get(TextId::Unknown));
}

std::wstring ServiceText(const Localization& strings, ServiceState state) {
    switch (state) {
        case ServiceState::Running: return std::wstring(strings.Get(TextId::Running));
        case ServiceState::Stopped: return std::wstring(strings.Get(TextId::Stopped));
        case ServiceState::NotInstalled: return std::wstring(strings.Get(TextId::NotInstalled));
        case ServiceState::StartPending: return L"Start pending";
        case ServiceState::StopPending: return L"Stop pending";
        case ServiceState::Paused: return L"Paused";
        case ServiceState::Unknown: return std::wstring(strings.Get(TextId::Unknown));
    }
    return std::wstring(strings.Get(TextId::Unknown));
}

std::wstring SourceText(const Localization& strings, const std::wstring& status) {
    if (status == L"ok") return std::wstring(strings.Get(TextId::SourceOk));
    if (status == L"stale") return std::wstring(strings.Get(TextId::SourceStale));
    if (status == L"error") return std::wstring(strings.Get(TextId::SourceError));
    if (status == L"disabled") return std::wstring(strings.Get(TextId::SourceDisabled));
    return std::wstring(strings.Get(TextId::Unknown));
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
    if (!CreateDeviceIndependentResources()) return 2;
    if (!RegisterWindowClass()) return 3;
    if (!CreateMainWindow(showCommand)) return 4;

    AddTrayIcon();
    StartPoller();

    MSG message{};
    while (GetMessageW(&message, nullptr, 0, 0) > 0) {
        TranslateMessage(&message);
        DispatchMessageW(&message);
    }
    return static_cast<int>(message.wParam);
}

bool App::CreateDeviceIndependentResources() {
    if (FAILED(D2D1CreateFactory(D2D1_FACTORY_TYPE_SINGLE_THREADED, d2dFactory_.GetAddressOf()))) {
        return false;
    }
    if (FAILED(DWriteCreateFactory(
            DWRITE_FACTORY_TYPE_SHARED,
            __uuidof(IDWriteFactory),
            reinterpret_cast<IUnknown**>(dwriteFactory_.GetAddressOf())))) {
        return false;
    }

    auto createFormat = [this](float size, DWRITE_FONT_WEIGHT weight, Microsoft::WRL::ComPtr<IDWriteTextFormat>& target) {
        return dwriteFactory_->CreateTextFormat(
            L"Segoe UI Variable",
            nullptr,
            weight,
            DWRITE_FONT_STYLE_NORMAL,
            DWRITE_FONT_STRETCH_NORMAL,
            size,
            L"",
            target.GetAddressOf()
        );
    };

    if (FAILED(createFormat(26.0f, DWRITE_FONT_WEIGHT_SEMI_BOLD, titleFormat_))) return false;
    if (FAILED(createFormat(18.0f, DWRITE_FONT_WEIGHT_SEMI_BOLD, sectionFormat_))) return false;
    if (FAILED(createFormat(30.0f, DWRITE_FONT_WEIGHT_SEMI_BOLD, heroFormat_))) return false;
    if (FAILED(createFormat(14.0f, DWRITE_FONT_WEIGHT_NORMAL, bodyFormat_))) return false;
    if (FAILED(createFormat(12.0f, DWRITE_FONT_WEIGHT_NORMAL, smallFormat_))) return false;

    for (auto* format : {titleFormat_.Get(), sectionFormat_.Get(), heroFormat_.Get(), bodyFormat_.Get(), smallFormat_.Get()}) {
        format->SetWordWrapping(DWRITE_WORD_WRAPPING_NO_WRAP);
    }
    return true;
}

bool App::RegisterWindowClass() {
    WNDCLASSEXW windowClass{};
    windowClass.cbSize = sizeof(windowClass);
    windowClass.style = CS_HREDRAW | CS_VREDRAW;
    windowClass.lpfnWndProc = &App::WindowProc;
    windowClass.hInstance = instance_;
    windowClass.hCursor = LoadCursorW(nullptr, IDC_ARROW);
    windowClass.hIcon = LoadIconW(nullptr, IDI_APPLICATION);
    windowClass.hbrBackground = nullptr;
    windowClass.lpszClassName = kWindowClass;
    return RegisterClassExW(&windowClass) != 0 || GetLastError() == ERROR_CLASS_ALREADY_EXISTS;
}

bool App::CreateMainWindow(int showCommand) {
    window_ = CreateWindowExW(
        0,
        kWindowClass,
        kWindowTitle,
        WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN,
        CW_USEDEFAULT,
        CW_USEDEFAULT,
        1240,
        780,
        nullptr,
        nullptr,
        instance_,
        this
    );
    if (window_ == nullptr) return false;

    const BOOL dark = TRUE;
    (void)DwmSetWindowAttribute(window_, DWMWA_USE_IMMERSIVE_DARK_MODE, &dark, sizeof(dark));

    ShowWindow(window_, showCommand == SW_HIDE ? SW_SHOWNORMAL : showCommand);
    UpdateWindow(window_);
    visible_.store(IsWindowVisible(window_) != FALSE, std::memory_order_relaxed);
    return true;
}

LRESULT CALLBACK App::WindowProc(HWND window, UINT message, WPARAM wParam, LPARAM lParam) {
    App* self = reinterpret_cast<App*>(GetWindowLongPtrW(window, GWLP_USERDATA));
    if (message == WM_NCCREATE) {
        const auto* create = reinterpret_cast<CREATESTRUCTW*>(lParam);
        self = static_cast<App*>(create->lpCreateParams);
        self->window_ = window;
        SetWindowLongPtrW(window, GWLP_USERDATA, reinterpret_cast<LONG_PTR>(self));
    }
    return self != nullptr
        ? self->HandleMessage(message, wParam, lParam)
        : DefWindowProcW(window, message, wParam, lParam);
}

LRESULT App::HandleMessage(UINT message, WPARAM wParam, LPARAM lParam) {
    switch (message) {
        case WM_PAINT: {
            PAINTSTRUCT paint{};
            BeginPaint(window_, &paint);
            Draw();
            EndPaint(window_, &paint);
            return 0;
        }
        case WM_SIZE:
            visible_.store(wParam != SIZE_MINIMIZED && IsWindowVisible(window_) != FALSE, std::memory_order_relaxed);
            Resize();
            return 0;
        case WM_SHOWWINDOW:
            visible_.store(wParam != FALSE, std::memory_order_relaxed);
            return 0;
        case WM_GETMINMAXINFO: {
            auto* info = reinterpret_cast<MINMAXINFO*>(lParam);
            info->ptMinTrackSize.x = 980;
            info->ptMinTrackSize.y = 620;
            return 0;
        }
        case WM_CLOSE:
            if (exitRequested_) {
                DestroyWindow(window_);
            } else {
                HideDashboard();
            }
            return 0;
        case WM_DESTROY:
            StopPoller();
            RemoveTrayIcon();
            PostQuitMessage(0);
            return 0;
        case kSnapshotMessage:
            InvalidateRect(window_, nullptr, FALSE);
            return 0;
        case kTrayMessage:
            if (lParam == WM_LBUTTONDBLCLK) {
                ShowDashboard();
            } else if (lParam == WM_RBUTTONUP || lParam == WM_CONTEXTMENU) {
                POINT point{};
                GetCursorPos(&point);
                ShowTrayMenu(point);
            }
            return 0;
        case WM_COMMAND:
            switch (LOWORD(wParam)) {
                case kTrayOpen:
                    ShowDashboard();
                    return 0;
                case kTrayExit:
                    exitRequested_ = true;
                    DestroyWindow(window_);
                    return 0;
                default:
                    break;
            }
            break;
        case WM_DPICHANGED: {
            const auto* rect = reinterpret_cast<RECT*>(lParam);
            SetWindowPos(
                window_, nullptr,
                rect->left, rect->top,
                rect->right - rect->left,
                rect->bottom - rect->top,
                SWP_NOACTIVATE | SWP_NOZORDER
            );
            DiscardRenderTarget();
            return 0;
        }
        default:
            break;
    }
    return DefWindowProcW(window_, message, wParam, lParam);
}

bool App::EnsureRenderTarget() {
    if (renderTarget_) return true;
    RECT rect{};
    GetClientRect(window_, &rect);
    const auto size = D2D1::SizeU(
        static_cast<UINT32>(std::max<LONG>(1, rect.right - rect.left)),
        static_cast<UINT32>(std::max<LONG>(1, rect.bottom - rect.top))
    );
    if (FAILED(d2dFactory_->CreateHwndRenderTarget(
            D2D1::RenderTargetProperties(),
            D2D1::HwndRenderTargetProperties(window_, size),
            renderTarget_.GetAddressOf()))) {
        return false;
    }

    const auto createBrush = [this](const D2D1_COLOR_F& color, Microsoft::WRL::ComPtr<ID2D1SolidColorBrush>& brush) {
        return renderTarget_->CreateSolidColorBrush(color, brush.GetAddressOf());
    };
    return SUCCEEDED(createBrush(kBackground, backgroundBrush_)) &&
           SUCCEEDED(createBrush(kSidebar, sidebarBrush_)) &&
           SUCCEEDED(createBrush(kSurface, surfaceBrush_)) &&
           SUCCEEDED(createBrush(kSurfaceStrong, surfaceStrongBrush_)) &&
           SUCCEEDED(createBrush(kBorder, borderBrush_)) &&
           SUCCEEDED(createBrush(kPrimary, primaryBrush_)) &&
           SUCCEEDED(createBrush(kSecondary, secondaryBrush_)) &&
           SUCCEEDED(createBrush(kAccent, accentBrush_)) &&
           SUCCEEDED(createBrush(kSuccess, successBrush_)) &&
           SUCCEEDED(createBrush(kWarning, warningBrush_)) &&
           SUCCEEDED(createBrush(kError, errorBrush_));
}

void App::DiscardRenderTarget() {
    errorBrush_.Reset();
    warningBrush_.Reset();
    successBrush_.Reset();
    accentBrush_.Reset();
    secondaryBrush_.Reset();
    primaryBrush_.Reset();
    borderBrush_.Reset();
    surfaceStrongBrush_.Reset();
    surfaceBrush_.Reset();
    sidebarBrush_.Reset();
    backgroundBrush_.Reset();
    renderTarget_.Reset();
}

void App::Resize() {
    if (!renderTarget_) return;
    RECT rect{};
    GetClientRect(window_, &rect);
    const auto size = D2D1::SizeU(
        static_cast<UINT32>(std::max<LONG>(1, rect.right - rect.left)),
        static_cast<UINT32>(std::max<LONG>(1, rect.bottom - rect.top))
    );
    if (FAILED(renderTarget_->Resize(size))) DiscardRenderTarget();
}

void App::DrawCard(ID2D1RenderTarget* target, const D2D1_RECT_F& rect, ID2D1Brush* fill, float radius) {
    target->FillRoundedRectangle(D2D1::RoundedRect(rect, radius, radius), fill);
    target->DrawRoundedRectangle(D2D1::RoundedRect(rect, radius, radius), borderBrush_.Get(), 1.0f);
}

void App::DrawTextBlock(
    ID2D1RenderTarget* target,
    std::wstring_view text,
    IDWriteTextFormat* format,
    const D2D1_RECT_F& rect,
    ID2D1Brush* brush,
    DWRITE_TEXT_ALIGNMENT alignment
) {
    if (text.empty()) return;
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
    if (!EnsureRenderTarget()) return;
    const auto snapshot = SnapshotCopy();
    const auto size = renderTarget_->GetSize();

    renderTarget_->BeginDraw();
    renderTarget_->Clear(kBackground);

    constexpr float sidebarWidth = 210.0f;
    constexpr float margin = 18.0f;
    constexpr float gap = 12.0f;
    const float contentLeft = sidebarWidth + margin;
    const float contentRight = size.width - margin;
    const float contentWidth = std::max(100.0f, contentRight - contentLeft);

    renderTarget_->FillRectangle(D2D1::RectF(0.0f, 0.0f, sidebarWidth, size.height), sidebarBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::AppTitle), titleFormat_.Get(), D2D1::RectF(22, 24, sidebarWidth - 16, 64), primaryBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::AppSubtitle), smallFormat_.Get(), D2D1::RectF(22, 66, sidebarWidth - 18, 105), secondaryBrush_.Get());

    float navY = 140.0f;
    for (const TextId id : {TextId::Dashboard, TextId::Sources, TextId::Diagnostics, TextId::Settings}) {
        const bool active = id == TextId::Dashboard;
        if (active) {
            renderTarget_->FillRoundedRectangle(
                D2D1::RoundedRect(D2D1::RectF(12, navY - 10, sidebarWidth - 12, navY + 36), 9, 9),
                surfaceStrongBrush_.Get()
            );
        }
        DrawTextBlock(renderTarget_.Get(), localization_.Get(id), bodyFormat_.Get(), D2D1::RectF(28, navY, sidebarWidth - 18, navY + 30), active ? accentBrush_.Get() : primaryBrush_.Get());
        navY += 62.0f;
    }

    const std::wstring connection = ConnectionText(localization_, snapshot.connection);
    const std::wstring service = ServiceText(localization_, snapshot.service);
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::Dashboard), titleFormat_.Get(), D2D1::RectF(contentLeft, 22, contentRight, 62), primaryBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), connection, bodyFormat_.Get(), D2D1::RectF(contentLeft, 66, contentRight, 96), snapshot.connection == ConnectionState::Live ? successBrush_.Get() : (snapshot.connection == ConnectionState::Offline || snapshot.connection == ConnectionState::ServerError || snapshot.connection == ConnectionState::SchemaError ? errorBrush_.Get() : warningBrush_.Get()));

    const float top = 112.0f;
    const float cardWidth = (contentWidth - gap * 3.0f) / 4.0f;
    struct SummaryCard { TextId label; std::wstring value; ID2D1Brush* brush; };
    const SummaryCard summaries[] = {
        {TextId::AgentStatus, connection, snapshot.connection == ConnectionState::Live ? successBrush_.Get() : warningBrush_.Get()},
        {TextId::WindowsService, service, snapshot.service == ServiceState::Running ? successBrush_.Get() : warningBrush_.Get()},
        {TextId::HubOutbox, snapshot.outbox.available ? std::to_wstring(snapshot.outbox.pendingEvents) : L"—", snapshot.outbox.status == L"warning" ? warningBrush_.Get() : accentBrush_.Get()},
        {TextId::AgentVersion, snapshot.agentVersion.empty() ? L"—" : snapshot.agentVersion, accentBrush_.Get()},
    };
    for (int i = 0; i < 4; ++i) {
        const float left = contentLeft + i * (cardWidth + gap);
        const D2D1_RECT_F card = D2D1::RectF(left, top, left + cardWidth, top + 118.0f);
        DrawCard(renderTarget_.Get(), card, surfaceBrush_.Get());
        DrawTextBlock(renderTarget_.Get(), localization_.Get(summaries[i].label), smallFormat_.Get(), D2D1::RectF(left + 16, top + 16, left + cardWidth - 12, top + 42), secondaryBrush_.Get());
        DrawTextBlock(renderTarget_.Get(), summaries[i].value, sectionFormat_.Get(), D2D1::RectF(left + 16, top + 52, left + cardWidth - 12, top + 92), summaries[i].brush);
    }

    const float mainTop = top + 118.0f + gap;
    const float mainBottom = std::min(size.height - margin, mainTop + 300.0f);
    const float taskWidth = contentWidth * 0.58f;
    const D2D1_RECT_F taskCard = D2D1::RectF(contentLeft, mainTop, contentLeft + taskWidth, mainBottom);
    DrawCard(renderTarget_.Get(), taskCard, surfaceBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::CurrentTask), sectionFormat_.Get(), D2D1::RectF(taskCard.left + 18, taskCard.top + 16, taskCard.right - 18, taskCard.top + 46), primaryBrush_.Get());

    const TaskView* task = CurrentTask(snapshot);
    if (task != nullptr) {
        DrawTextBlock(renderTarget_.Get(), task->title, heroFormat_.Get(), D2D1::RectF(taskCard.left + 18, taskCard.top + 62, taskCard.right - 18, taskCard.top + 112), accentBrush_.Get());
        std::wstring meta = task->status;
        if (!task->workspace.empty()) meta += L"  ·  " + task->workspace;
        if (task->durationSeconds) meta += L"  ·  " + FormatDuration(*task->durationSeconds);
        DrawTextBlock(renderTarget_.Get(), meta, bodyFormat_.Get(), D2D1::RectF(taskCard.left + 18, taskCard.top + 128, taskCard.right - 18, taskCard.top + 158), secondaryBrush_.Get());
        if (!task->activity.empty()) {
            DrawTextBlock(renderTarget_.Get(), task->activity, sectionFormat_.Get(), D2D1::RectF(taskCard.left + 18, taskCard.top + 180, taskCard.right - 18, taskCard.bottom - 18), primaryBrush_.Get());
        }
    } else {
        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::NoCurrentTask), bodyFormat_.Get(), D2D1::RectF(taskCard.left + 18, taskCard.top + 76, taskCard.right - 18, taskCard.bottom - 18), secondaryBrush_.Get());
    }

    const D2D1_RECT_F commandCard = D2D1::RectF(taskCard.right + gap, mainTop, contentRight, mainBottom);
    DrawCard(renderTarget_.Get(), commandCard, surfaceStrongBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::CommandCode), sectionFormat_.Get(), D2D1::RectF(commandCard.left + 18, commandCard.top + 16, commandCard.right - 18, commandCard.top + 46), primaryBrush_.Get());
    const std::wstring plan = snapshot.plan.empty() ? L"—" : snapshot.plan;
    DrawTextBlock(renderTarget_.Get(), plan, bodyFormat_.Get(), D2D1::RectF(commandCard.left + 18, commandCard.top + 54, commandCard.right - 18, commandCard.top + 84), accentBrush_.Get());

    std::wstring credit = L"—";
    if (snapshot.creditRemaining) {
        credit = FormatNumber(*snapshot.creditRemaining);
        if (snapshot.creditLimit) credit += L" / " + FormatNumber(*snapshot.creditLimit);
        if (!snapshot.creditUnit.empty()) credit += L" " + snapshot.creditUnit;
    }
    DrawTextBlock(renderTarget_.Get(), credit, heroFormat_.Get(), D2D1::RectF(commandCard.left + 18, commandCard.top + 98, commandCard.right - 18, commandCard.top + 150), successBrush_.Get());
    if (const auto percent = CreditRemainingPercent(snapshot)) {
        const float barLeft = commandCard.left + 18;
        const float barTop = commandCard.top + 168;
        const float barRight = commandCard.right - 18;
        renderTarget_->FillRoundedRectangle(D2D1::RoundedRect(D2D1::RectF(barLeft, barTop, barRight, barTop + 10), 5, 5), borderBrush_.Get());
        const float filled = barLeft + (barRight - barLeft) * static_cast<float>(*percent / 100.0);
        renderTarget_->FillRoundedRectangle(D2D1::RoundedRect(D2D1::RectF(barLeft, barTop, std::max(barLeft + 2.0f, filled), barTop + 10), 5, 5), accentBrush_.Get());
        const std::wstring percentText = FormatNumber(*percent) + L"%";
        DrawTextBlock(renderTarget_.Get(), percentText, bodyFormat_.Get(), D2D1::RectF(barLeft, barTop + 20, barRight, barTop + 50), secondaryBrush_.Get(), DWRITE_TEXT_ALIGNMENT_TRAILING);
    }

    const float lowerTop = mainBottom + gap;
    if (lowerTop + 100.0f < size.height - margin) {
        const D2D1_RECT_F sourceCard = D2D1::RectF(contentLeft, lowerTop, contentRight, size.height - margin);
        DrawCard(renderTarget_.Get(), sourceCard, surfaceBrush_.Get());
        const float half = (sourceCard.left + sourceCard.right) / 2.0f;
        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::ZCode), sectionFormat_.Get(), D2D1::RectF(sourceCard.left + 18, sourceCard.top + 16, half - gap, sourceCard.top + 44), primaryBrush_.Get());
        DrawTextBlock(renderTarget_.Get(), SourceText(localization_, snapshot.zcodeStatus), bodyFormat_.Get(), D2D1::RectF(sourceCard.left + 18, sourceCard.top + 54, half - gap, sourceCard.top + 86), snapshot.zcodeStatus == L"ok" ? successBrush_.Get() : warningBrush_.Get());
        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::CommandCode), sectionFormat_.Get(), D2D1::RectF(half + gap, sourceCard.top + 16, sourceCard.right - 18, sourceCard.top + 44), primaryBrush_.Get());
        DrawTextBlock(renderTarget_.Get(), SourceText(localization_, snapshot.commandCodeStatus), bodyFormat_.Get(), D2D1::RectF(half + gap, sourceCard.top + 54, sourceCard.right - 18, sourceCard.top + 86), snapshot.commandCodeStatus == L"ok" ? successBrush_.Get() : warningBrush_.Get());
        if (snapshot.uptimeSeconds) {
            const std::wstring uptime = std::wstring(localization_.Get(TextId::Uptime)) + L": " + FormatDuration(*snapshot.uptimeSeconds);
            DrawTextBlock(renderTarget_.Get(), uptime, smallFormat_.Get(), D2D1::RectF(sourceCard.left + 18, sourceCard.bottom - 34, sourceCard.right - 18, sourceCard.bottom - 10), secondaryBrush_.Get());
        }
    }

    const HRESULT result = renderTarget_->EndDraw();
    if (result == D2DERR_RECREATE_TARGET) DiscardRenderTarget();
}

void App::AddTrayIcon() {
    if (trayAdded_ || window_ == nullptr) return;
    tray_ = {};
    tray_.cbSize = sizeof(tray_);
    tray_.hWnd = window_;
    tray_.uID = 1;
    tray_.uFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP;
    tray_.uCallbackMessage = kTrayMessage;
    tray_.hIcon = LoadIconW(nullptr, IDI_APPLICATION);
    wcscpy_s(tray_.szTip, L"AI Control HUD — Agent");
    trayAdded_ = Shell_NotifyIconW(NIM_ADD, &tray_) != FALSE;
}

void App::RemoveTrayIcon() {
    if (!trayAdded_) return;
    Shell_NotifyIconW(NIM_DELETE, &tray_);
    trayAdded_ = false;
}

void App::ShowTrayMenu(POINT point) {
    HMENU menu = CreatePopupMenu();
    if (menu == nullptr) return;
    AppendMenuW(menu, MF_STRING, kTrayOpen, localization_.Get(TextId::OpenDashboard).data());
    AppendMenuW(menu, MF_SEPARATOR, 0, nullptr);
    AppendMenuW(menu, MF_STRING, kTrayExit, localization_.Get(TextId::Exit).data());
    SetForegroundWindow(window_);
    TrackPopupMenu(menu, TPM_RIGHTBUTTON | TPM_BOTTOMALIGN, point.x, point.y, 0, window_, nullptr);
    DestroyMenu(menu);
}

void App::ShowDashboard() {
    ShowWindow(window_, SW_RESTORE);
    SetForegroundWindow(window_);
    visible_.store(true, std::memory_order_relaxed);
}

void App::HideDashboard() {
    ShowWindow(window_, SW_HIDE);
    visible_.store(false, std::memory_order_relaxed);
}

DashboardSnapshot App::SnapshotCopy() const {
    std::lock_guard lock(snapshotMutex_);
    return snapshot_;
}

void App::StartPoller() {
    if (poller_.joinable()) return;
    poller_ = std::jthread([this](std::stop_token stopToken) {
        winrt::init_apartment(winrt::apartment_type::multi_threaded);
        std::unique_ptr<AgentClient> client;
        std::condition_variable_any condition;
        std::mutex waitMutex;

        while (!stopToken.stop_requested()) {
            DashboardSnapshot next;
            try {
                if (!client) client = std::make_unique<AgentClient>();
                next = client->Poll();
            } catch (const std::exception&) {
                client.reset();
                next.connection = ConnectionState::Offline;
                next.service = QueryAgentServiceState();
                next.error = L"local Agent unavailable";
            }

            {
                std::lock_guard lock(snapshotMutex_);
                snapshot_ = std::move(next);
            }
            if (window_ != nullptr) PostMessageW(window_, kSnapshotMessage, 0, 0);

            const auto delay = visible_.load(std::memory_order_relaxed)
                ? std::chrono::seconds(2)
                : std::chrono::seconds(10);
            std::unique_lock lock(waitMutex);
            condition.wait_for(lock, stopToken, delay, [] { return false; });
        }
        client.reset();
        winrt::uninit_apartment();
    });
}

void App::StopPoller() {
    if (!poller_.joinable()) return;
    poller_.request_stop();
    poller_.join();
}

}  // namespace aicontrol::ui
