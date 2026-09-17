#include "app.h"

#include "agent_client.h"
#include "health_probe.h"
#include "privileged_actions.h"
#include "window_state.h"

#include <dwmapi.h>
#include <shellapi.h>
#include <winrt/base.h>

#include <algorithm>
#include <chrono>
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
constexpr float kNavTop = 132.0f;
constexpr float kNavHeight = 40.0f;
constexpr float kNavGap = 8.0f;
constexpr int kMiniWidth = 600;
constexpr int kMiniHeight = 238;

D2D1_COLOR_F Rgb(UINT32 rgb, float alpha = 1.0f) noexcept {
    return D2D1::ColorF(rgb, alpha);
}

D2D1_COLOR_F SystemColor(int index) noexcept {
    const COLORREF value = GetSysColor(index);
    return D2D1::ColorF(
        static_cast<float>(GetRValue(value)) / 255.0f,
        static_cast<float>(GetGValue(value)) / 255.0f,
        static_cast<float>(GetBValue(value)) / 255.0f,
        1.0f
    );
}

bool HighContrastEnabled() noexcept {
    HIGHCONTRASTW highContrast{};
    highContrast.cbSize = sizeof(highContrast);
    return SystemParametersInfoW(SPI_GETHIGHCONTRAST, sizeof(highContrast), &highContrast, 0) != FALSE &&
           (highContrast.dwFlags & HCF_HIGHCONTRASTON) != 0;
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
    if (!CreateDeviceIndependentResources() || !RegisterWindowClass() || !CreateMainWindow(showCommand)) return 1;

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
    const WindowState restored = LoadWindowState();
    int x = CW_USEDEFAULT;
    int y = CW_USEDEFAULT;
    int width = 1120;
    int height = 720;
    if (restored.hasNormalRect && MonitorFromRect(&restored.normalRect, MONITOR_DEFAULTTONULL) != nullptr) {
        normalRect_ = restored.normalRect;
        hasNormalRect_ = true;
        x = normalRect_.left;
        y = normalRect_.top;
        width = normalRect_.right - normalRect_.left;
        height = normalRect_.bottom - normalRect_.top;
    }

    const std::wstring title(localization_.Get(TextId::AppTitle));
    window_ = CreateWindowExW(0, kWindowClass, title.c_str(), WS_OVERLAPPEDWINDOW,
                              x, y, width, height, nullptr, nullptr, instance_, this);
    if (window_ == nullptr) return false;

    if (!HighContrastEnabled()) {
        BOOL dark = TRUE;
        constexpr DWORD kImmersiveDarkMode = 20;
        (void)DwmSetWindowAttribute(window_, kImmersiveDarkMode, &dark, sizeof(dark));
    }

    ShowWindow(window_, showCommand == SW_HIDE ? SW_SHOWNORMAL : showCommand);
    UpdateWindow(window_);
    visible_.store(IsWindowVisible(window_) != FALSE, std::memory_order_relaxed);
    if (restored.miniHud) ApplyMiniHud(true);
    return true;
}

bool App::CreateDeviceIndependentResources() {
    HRESULT hr = D2D1CreateFactory(D2D1_FACTORY_TYPE_SINGLE_THREADED, d2dFactory_.GetAddressOf());
    if (FAILED(hr)) return false;
    hr = DWriteCreateFactory(DWRITE_FACTORY_TYPE_SHARED, __uuidof(IDWriteFactory),
                             reinterpret_cast<IUnknown**>(dwriteFactory_.GetAddressOf()));
    if (FAILED(hr)) return false;

    const wchar_t* localeName = localization_.IsSimplifiedChinese() ? L"zh-CN" : L"en-US";
    const auto createFormat = [&](float size, DWRITE_FONT_WEIGHT weight, auto& target) -> bool {
        return SUCCEEDED(dwriteFactory_->CreateTextFormat(
            L"Segoe UI", nullptr, weight, DWRITE_FONT_STYLE_NORMAL,
            DWRITE_FONT_STRETCH_NORMAL, size, localeName, target.GetAddressOf()));
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
    const HRESULT hr = d2dFactory_->CreateHwndRenderTarget(
        D2D1::RenderTargetProperties(),
        D2D1::HwndRenderTargetProperties(window_, size),
        renderTarget_.GetAddressOf()
    );
    if (FAILED(hr)) return false;

    const bool highContrast = HighContrastEnabled();
    const auto createBrush = [&](D2D1_COLOR_F color, auto& target) -> bool {
        return SUCCEEDED(renderTarget_->CreateSolidColorBrush(color, target.GetAddressOf()));
    };
    const D2D1_COLOR_F background = highContrast ? SystemColor(COLOR_WINDOW) : Rgb(0x0B0F14);
    const D2D1_COLOR_F surface = highContrast ? SystemColor(COLOR_WINDOW) : Rgb(0x151D27);
    const D2D1_COLOR_F strong = highContrast ? SystemColor(COLOR_WINDOW) : Rgb(0x182432);
    const D2D1_COLOR_F text = highContrast ? SystemColor(COLOR_WINDOWTEXT) : Rgb(0xF2F7FB);
    const D2D1_COLOR_F secondary = highContrast ? SystemColor(COLOR_WINDOWTEXT) : Rgb(0x91A2B4);
    const D2D1_COLOR_F accent = highContrast ? SystemColor(COLOR_HIGHLIGHT) : Rgb(0x52B8FF);

    if (!createBrush(background, backgroundBrush_) ||
        !createBrush(highContrast ? background : Rgb(0x101720), sidebarBrush_) ||
        !createBrush(surface, surfaceBrush_) ||
        !createBrush(strong, surfaceStrongBrush_) ||
        !createBrush(highContrast ? text : Rgb(0x293544), borderBrush_) ||
        !createBrush(text, primaryBrush_) ||
        !createBrush(secondary, secondaryBrush_) ||
        !createBrush(accent, accentBrush_) ||
        !createBrush(highContrast ? text : Rgb(0x52D3A1), successBrush_) ||
        !createBrush(highContrast ? text : Rgb(0xF4C75D), warningBrush_) ||
        !createBrush(highContrast ? text : Rgb(0xFF6B78), errorBrush_)) {
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

void App::DrawTextBlock(ID2D1RenderTarget* target, std::wstring_view text, IDWriteTextFormat* format,
                        const D2D1_RECT_F& rect, ID2D1Brush* brush, DWRITE_TEXT_ALIGNMENT alignment) {
    if (text.empty() || format == nullptr || brush == nullptr) return;
    format->SetTextAlignment(alignment);
    target->DrawTextW(text.data(), static_cast<UINT32>(text.size()), format, rect, brush, D2D1_DRAW_TEXT_OPTIONS_CLIP);
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
    renderTarget_->Clear(HighContrastEnabled() ? SystemColor(COLOR_WINDOW) : Rgb(0x0B0F14));

    const auto finish = [&]() {
        const HRESULT hr = renderTarget_->EndDraw();
        if (hr == D2DERR_RECREATE_TARGET) DiscardRenderTarget();
        EndPaint(window_, &paint);
    };

    const std::wstring connection = ConnectionText(localization_, snapshot.connection);
    ID2D1Brush* connectionBrush = secondaryBrush_.Get();
    if (snapshot.connection == ConnectionState::Live) connectionBrush = successBrush_.Get();
    else if (snapshot.connection == ConnectionState::Degraded || snapshot.connection == ConnectionState::Connecting) connectionBrush = warningBrush_.Get();
    else connectionBrush = errorBrush_.Get();

    if (miniHud_) {
        const float left = 16.0f;
        const float top = 14.0f;
        const float right = std::max(left + 300.0f, size.width - 16.0f);
        const float bottom = std::max(top + 150.0f, size.height - 14.0f);
        DrawCard(renderTarget_.Get(), D2D1::RectF(left, top, right, bottom), surfaceStrongBrush_.Get(), 12.0f);
        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::MiniHud), sectionFormat_.Get(),
                      D2D1::RectF(left + 16, top + 12, right - 150, top + 42), primaryBrush_.Get());
        DrawTextBlock(renderTarget_.Get(), connection, bodyFormat_.Get(),
                      D2D1::RectF(right - 140, top + 14, right - 16, top + 40), connectionBrush,
                      DWRITE_TEXT_ALIGNMENT_TRAILING);

        const float mid = left + (right - left) * 0.54f;
        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::CurrentTask), smallFormat_.Get(),
                      D2D1::RectF(left + 16, top + 56, mid - 12, top + 78), secondaryBrush_.Get());
        const TaskView* current = CurrentTask(snapshot);
        if (current == nullptr) {
            DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::NoCurrentTask), bodyFormat_.Get(),
                          D2D1::RectF(left + 16, top + 84, mid - 12, bottom - 20), primaryBrush_.Get());
        } else {
            DrawTextBlock(renderTarget_.Get(), current->title, sectionFormat_.Get(),
                          D2D1::RectF(left + 16, top + 82, mid - 12, top + 114), primaryBrush_.Get());
            std::wstring meta = current->status;
            if (!current->workspace.empty()) meta += L" · " + current->workspace;
            DrawTextBlock(renderTarget_.Get(), meta, smallFormat_.Get(),
                          D2D1::RectF(left + 16, top + 120, mid - 12, bottom - 18), accentBrush_.Get());
        }

        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::CommandCode), smallFormat_.Get(),
                      D2D1::RectF(mid + 10, top + 56, right - 16, top + 78), secondaryBrush_.Get());
        const std::wstring plan = snapshot.plan.empty() ? L"—" : snapshot.plan;
        DrawTextBlock(renderTarget_.Get(), plan, bodyFormat_.Get(),
                      D2D1::RectF(mid + 10, top + 82, right - 16, top + 106), accentBrush_.Get());
        std::wstring credit = L"—";
        if (snapshot.creditRemaining) {
            credit = ToFixed(*snapshot.creditRemaining, 2);
            if (!snapshot.creditUnit.empty()) credit += L" " + snapshot.creditUnit;
        }
        DrawTextBlock(renderTarget_.Get(), credit, sectionFormat_.Get(),
                      D2D1::RectF(mid + 10, top + 112, right - 16, top + 142), successBrush_.Get());
        const std::wstring health = L"ZCode " + SourceText(localization_, snapshot.zcodeStatus) +
                                    L" · CommandCode " + SourceText(localization_, snapshot.commandCodeStatus);
        DrawTextBlock(renderTarget_.Get(), health, smallFormat_.Get(),
                      D2D1::RectF(mid + 10, top + 148, right - 16, bottom - 16), secondaryBrush_.Get());
        finish();
        return;
    }

    renderTarget_->FillRectangle(D2D1::RectF(0, 0, kSidebarWidth, size.height), sidebarBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::AppTitle), sectionFormat_.Get(),
                  D2D1::RectF(22, 26, kSidebarWidth - 18, 62), primaryBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::AppSubtitle), smallFormat_.Get(),
                  D2D1::RectF(22, 66, kSidebarWidth - 18, 114), secondaryBrush_.Get());

    const struct NavItem { TextId text; Page page; } nav[] = {
        {TextId::Dashboard, Page::Dashboard},
        {TextId::Sources, Page::Sources},
        {TextId::Diagnostics, Page::Diagnostics},
    };
    for (std::size_t i = 0; i < 3; ++i) {
        const float navTop = kNavTop + static_cast<float>(i) * (kNavHeight + kNavGap);
        if (page_ == nav[i].page) {
            renderTarget_->FillRoundedRectangle(
                D2D1::RoundedRect(D2D1::RectF(12, navTop, kSidebarWidth - 12, navTop + kNavHeight), 9, 9),
                surfaceStrongBrush_.Get());
        }
        DrawTextBlock(renderTarget_.Get(), localization_.Get(nav[i].text), bodyFormat_.Get(),
                      D2D1::RectF(26, navTop + 9, kSidebarWidth - 20, navTop + 34),
                      page_ == nav[i].page ? accentBrush_.Get() : primaryBrush_.Get());
    }

    const float left = kSidebarWidth + kOuterGap;
    const float right = std::max(left + 520.0f, size.width - kOuterGap);
    const TextId pageTitle = page_ == Page::Dashboard ? TextId::Dashboard :
                             page_ == Page::Sources ? TextId::Sources : TextId::Diagnostics;
    DrawTextBlock(renderTarget_.Get(), localization_.Get(pageTitle), titleFormat_.Get(),
                  D2D1::RectF(left, 22, right - 220, 66), primaryBrush_.Get());
    DrawTextBlock(renderTarget_.Get(), connection, sectionFormat_.Get(),
                  D2D1::RectF(right - 210, 26, right, 58), connectionBrush, DWRITE_TEXT_ALIGNMENT_TRAILING);

    if (page_ == Page::Sources) {
        const float top = 82.0f;
        const float width = (right - left - kCardGap) / 2.0f;
        const D2D1_RECT_F zRect = D2D1::RectF(left, top, left + width, size.height - kOuterGap);
        const D2D1_RECT_F cRect = D2D1::RectF(left + width + kCardGap, top, right, size.height - kOuterGap);
        DrawCard(renderTarget_.Get(), zRect, surfaceBrush_.Get());
        DrawCard(renderTarget_.Get(), cRect, surfaceStrongBrush_.Get());

        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::ZCode), sectionFormat_.Get(),
                      D2D1::RectF(zRect.left + 20, zRect.top + 18, zRect.right - 20, zRect.top + 48), primaryBrush_.Get());
        DrawTextBlock(renderTarget_.Get(), SourceText(localization_, snapshot.zcodeStatus), bodyFormat_.Get(),
                      D2D1::RectF(zRect.left + 20, zRect.top + 54, zRect.right - 20, zRect.top + 82),
                      snapshot.zcodeStatus == L"ok" ? successBrush_.Get() : warningBrush_.Get());
        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::TaskList), smallFormat_.Get(),
                      D2D1::RectF(zRect.left + 20, zRect.top + 102, zRect.right - 20, zRect.top + 126), secondaryBrush_.Get());

        float rowY = zRect.top + 136.0f;
        const std::size_t taskCount = std::min<std::size_t>(7, snapshot.tasks.size());
        if (taskCount == 0) {
            DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::NoData), bodyFormat_.Get(),
                          D2D1::RectF(zRect.left + 20, rowY, zRect.right - 20, rowY + 36), secondaryBrush_.Get());
        }
        for (std::size_t i = 0; i < taskCount; ++i) {
            const auto& task = snapshot.tasks[i];
            DrawTextBlock(renderTarget_.Get(), task.title, bodyFormat_.Get(),
                          D2D1::RectF(zRect.left + 20, rowY, zRect.right - 150, rowY + 24), primaryBrush_.Get());
            DrawTextBlock(renderTarget_.Get(), task.status, smallFormat_.Get(),
                          D2D1::RectF(zRect.right - 142, rowY + 1, zRect.right - 20, rowY + 24), accentBrush_.Get(),
                          DWRITE_TEXT_ALIGNMENT_TRAILING);
            std::wstring meta = task.workspace;
            if (task.durationSeconds) meta += (meta.empty() ? L"" : L" · ") + DurationText(*task.durationSeconds);
            DrawTextBlock(renderTarget_.Get(), meta, smallFormat_.Get(),
                          D2D1::RectF(zRect.left + 20, rowY + 26, zRect.right - 20, rowY + 48), secondaryBrush_.Get());
            rowY += 54.0f;
        }

        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::CommandCode), sectionFormat_.Get(),
                      D2D1::RectF(cRect.left + 20, cRect.top + 18, cRect.right - 20, cRect.top + 48), primaryBrush_.Get());
        DrawTextBlock(renderTarget_.Get(), SourceText(localization_, snapshot.commandCodeStatus), bodyFormat_.Get(),
                      D2D1::RectF(cRect.left + 20, cRect.top + 54, cRect.right - 20, cRect.top + 82),
                      snapshot.commandCodeStatus == L"ok" ? successBrush_.Get() : warningBrush_.Get());
        const std::wstring plan = std::wstring(localization_.Get(TextId::Plan)) + L": " +
                                  (snapshot.plan.empty() ? L"—" : snapshot.plan);
        DrawTextBlock(renderTarget_.Get(), plan, bodyFormat_.Get(),
                      D2D1::RectF(cRect.left + 20, cRect.top + 94, cRect.right - 20, cRect.top + 122), accentBrush_.Get());
        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::UsageWindows), smallFormat_.Get(),
                      D2D1::RectF(cRect.left + 20, cRect.top + 136, cRect.right - 20, cRect.top + 160), secondaryBrush_.Get());

        rowY = cRect.top + 172.0f;
        const std::size_t windowCount = std::min<std::size_t>(6, snapshot.usageWindows.size());
        if (windowCount == 0) {
            DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::NoData), bodyFormat_.Get(),
                          D2D1::RectF(cRect.left + 20, rowY, cRect.right - 20, rowY + 36), secondaryBrush_.Get());
        }
        for (std::size_t i = 0; i < windowCount; ++i) {
            const auto& usage = snapshot.usageWindows[i];
            const std::wstring value = usage.usedPercent ? ToFixed(*usage.usedPercent, 1) + L"%" : L"—";
            DrawTextBlock(renderTarget_.Get(), usage.name, bodyFormat_.Get(),
                          D2D1::RectF(cRect.left + 20, rowY, cRect.right - 132, rowY + 24), primaryBrush_.Get());
            DrawTextBlock(renderTarget_.Get(), value, bodyFormat_.Get(),
                          D2D1::RectF(cRect.right - 126, rowY, cRect.right - 20, rowY + 24), accentBrush_.Get(),
                          DWRITE_TEXT_ALIGNMENT_TRAILING);
            if (!usage.resetAt.empty()) {
                const std::wstring reset = std::wstring(localization_.Get(TextId::ResetAt)) + L": " + usage.resetAt;
                DrawTextBlock(renderTarget_.Get(), reset, smallFormat_.Get(),
                              D2D1::RectF(cRect.left + 20, rowY + 27, cRect.right - 20, rowY + 48), secondaryBrush_.Get());
            }
            rowY += 58.0f;
        }
        finish();
        return;
    }

    if (page_ == Page::Diagnostics) {
        const float top = 82.0f;
        const float width = (right - left - kCardGap) / 2.0f;
        const D2D1_RECT_F agentRect = D2D1::RectF(left, top, left + width, size.height - kOuterGap);
        const D2D1_RECT_F outboxRect = D2D1::RectF(left + width + kCardGap, top, right, size.height - kOuterGap);
        DrawCard(renderTarget_.Get(), agentRect, surfaceBrush_.Get());
        DrawCard(renderTarget_.Get(), outboxRect, surfaceBrush_.Get());

        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::AgentStatus), sectionFormat_.Get(),
                      D2D1::RectF(agentRect.left + 20, agentRect.top + 18, agentRect.right - 20, agentRect.top + 48), primaryBrush_.Get());
        float rowY = agentRect.top + 66.0f;
        const std::wstring service = std::wstring(localization_.Get(TextId::WindowsService)) + L": " + ServiceText(localization_, snapshot.service);
        DrawTextBlock(renderTarget_.Get(), service, bodyFormat_.Get(),
                      D2D1::RectF(agentRect.left + 20, rowY, agentRect.right - 20, rowY + 28), secondaryBrush_.Get());
        rowY += 38.0f;
        const std::wstring version = std::wstring(localization_.Get(TextId::AgentVersion)) + L": " +
                                     (snapshot.agentVersion.empty() ? L"—" : snapshot.agentVersion);
        DrawTextBlock(renderTarget_.Get(), version, bodyFormat_.Get(),
                      D2D1::RectF(agentRect.left + 20, rowY, agentRect.right - 20, rowY + 28), secondaryBrush_.Get());
        rowY += 38.0f;
        const std::wstring uptime = std::wstring(localization_.Get(TextId::Uptime)) + L": " +
                                    (snapshot.uptimeSeconds ? DurationText(*snapshot.uptimeSeconds) : L"—");
        DrawTextBlock(renderTarget_.Get(), uptime, bodyFormat_.Get(),
                      D2D1::RectF(agentRect.left + 20, rowY, agentRect.right - 20, rowY + 28), secondaryBrush_.Get());
        rowY += 38.0f;
        const std::wstring localApi = std::wstring(localization_.Get(TextId::LocalApi)) + L": 127.0.0.1:8787";
        DrawTextBlock(renderTarget_.Get(), localApi, bodyFormat_.Get(),
                      D2D1::RectF(agentRect.left + 20, rowY, agentRect.right - 20, rowY + 28), accentBrush_.Get());
        rowY += 46.0f;
        if (!snapshot.error.empty()) {
            DrawTextBlock(renderTarget_.Get(), snapshot.error, smallFormat_.Get(),
                          D2D1::RectF(agentRect.left + 20, rowY, agentRect.right - 20, agentRect.bottom - 20), errorBrush_.Get());
        }

        DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::HubOutbox), sectionFormat_.Get(),
                      D2D1::RectF(outboxRect.left + 20, outboxRect.top + 18, outboxRect.right - 20, outboxRect.top + 48), primaryBrush_.Get());
        rowY = outboxRect.top + 66.0f;
        if (!snapshot.outbox.available) {
            DrawTextBlock(renderTarget_.Get(), localization_.Get(TextId::NoData), bodyFormat_.Get(),
                          D2D1::RectF(outboxRect.left + 20, rowY, outboxRect.right - 20, rowY + 36), secondaryBrush_.Get());
        } else {
            const std::wstring pending = std::wstring(localization_.Get(TextId::OutboxPending)) + L": " +
                                         std::to_wstring(snapshot.outbox.pendingEvents);
            DrawTextBlock(renderTarget_.Get(), pending, bodyFormat_.Get(),
                          D2D1::RectF(outboxRect.left + 20, rowY, outboxRect.right - 20, rowY + 28), accentBrush_.Get());
            rowY += 38.0f;
            const std::wstring baselines = std::wstring(localization_.Get(TextId::OutboxBaselines)) + L": " +
                                           std::to_wstring(snapshot.outbox.taskBaselineRows);
            DrawTextBlock(renderTarget_.Get(), baselines, bodyFormat_.Get(),
                          D2D1::RectF(outboxRect.left + 20, rowY, outboxRect.right - 20, rowY + 28), secondaryBrush_.Get());
            rowY += 38.0f;
            const std::wstring compacted = std::wstring(localization_.Get(TextId::OutboxCompacted)) + L": " +
                                           std::to_wstring(snapshot.outbox.compactedTaskRows);
            DrawTextBlock(renderTarget_.Get(), compacted, bodyFormat_.Get(),
                          D2D1::RectF(outboxRect.left + 20, rowY, outboxRect.right - 20, rowY + 28), secondaryBrush_.Get());
            rowY += 38.0f;
            DrawTextBlock(renderTarget_.Get(), snapshot.outbox.status, bodyFormat_.Get(),
                          D2D1::RectF(outboxRect.left + 20, rowY, outboxRect.right - 20, rowY + 28), warningBrush_.Get());
        }
        finish();
        return;
    }

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
    std::wstring diagnostics = std::wstring(localization_.Get(TextId::AgentVersion)) + L"  " +
                               (snapshot.agentVersion.empty() ? L"—" : snapshot.agentVersion);
    if (snapshot.uptimeSeconds) diagnostics += L"  ·  " + DurationText(*snapshot.uptimeSeconds);
    DrawTextBlock(renderTarget_.Get(), diagnostics, bodyFormat_.Get(),
                  D2D1::RectF(opsRect.left + 20, opsRect.top + 88, opsRect.right - 20, opsRect.top + 116), secondaryBrush_.Get());
    if (snapshot.outbox.available) {
        const std::wstring outbox = std::wstring(localization_.Get(TextId::OutboxPending)) + L"  " +
                                    std::to_wstring(snapshot.outbox.pendingEvents);
        DrawTextBlock(renderTarget_.Get(), outbox, smallFormat_.Get(),
                      D2D1::RectF(opsRect.left + 20, opsRect.top + 119, opsRect.right - 20, opsRect.bottom - 10), secondaryBrush_.Get());
    }

    finish();
}

void App::Resize() {
    if (!renderTarget_ || window_ == nullptr) return;
    RECT client{};
    GetClientRect(window_, &client);
    (void)renderTarget_->Resize(D2D1::SizeU(
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
    const std::wstring mini(localization_.Get(miniHud_ ? TextId::FullDashboard : TextId::MiniHud));
    const std::wstring actions(localization_.Get(TextId::ServiceActions));
    const std::wstring start(localization_.Get(TextId::ServiceStartAction));
    const std::wstring stop(localization_.Get(TextId::ServiceStopAction));
    const std::wstring restart(localization_.Get(TextId::ServiceRestartAction));
    const std::wstring upgrade(localization_.Get(TextId::ServiceUpgradeAction));
    const std::wstring exit(localization_.Get(TextId::Exit));

    AppendMenuW(menu, MF_STRING, kTrayOpen, open.c_str());
    AppendMenuW(menu, MF_STRING, kTrayMiniHud, mini.c_str());
    HMENU serviceMenu = CreatePopupMenu();
    if (serviceMenu != nullptr) {
        AppendMenuW(serviceMenu, MF_STRING, kTrayServiceStart, start.c_str());
        AppendMenuW(serviceMenu, MF_STRING, kTrayServiceStop, stop.c_str());
        AppendMenuW(serviceMenu, MF_STRING, kTrayServiceRestart, restart.c_str());
        AppendMenuW(serviceMenu, MF_SEPARATOR, 0, nullptr);
        AppendMenuW(serviceMenu, MF_STRING, kTrayServiceUpgrade, upgrade.c_str());
        AppendMenuW(menu, MF_POPUP, reinterpret_cast<UINT_PTR>(serviceMenu), actions.c_str());
    }
    AppendMenuW(menu, MF_SEPARATOR, 0, nullptr);
    AppendMenuW(menu, MF_STRING, kTrayExit, exit.c_str());
    SetForegroundWindow(window_);
    TrackPopupMenu(menu, TPM_RIGHTBUTTON | TPM_BOTTOMALIGN | TPM_LEFTALIGN, point.x, point.y, 0, window_, nullptr);
    DestroyMenu(menu);
}

void App::RunPrivilegedCommand(UINT commandId) {
    PrivilegedAction action{};
    TextId actionText = TextId::ServiceRestartAction;
    switch (commandId) {
        case kTrayServiceStart:
            action = PrivilegedAction::Start;
            actionText = TextId::ServiceStartAction;
            break;
        case kTrayServiceStop:
            action = PrivilegedAction::Stop;
            actionText = TextId::ServiceStopAction;
            break;
        case kTrayServiceRestart:
            action = PrivilegedAction::Restart;
            actionText = TextId::ServiceRestartAction;
            break;
        case kTrayServiceUpgrade:
            action = PrivilegedAction::Upgrade;
            actionText = TextId::ServiceUpgradeAction;
            break;
        default:
            return;
    }

    const std::wstring title(localization_.Get(TextId::ServiceActions));
    const std::wstring prompt = std::wstring(localization_.Get(actionText)) + L"\n\n" +
                                std::wstring(localization_.Get(TextId::ConfirmPrivileged));
    if (MessageBoxW(window_, prompt.c_str(), title.c_str(), MB_YESNO | MB_ICONWARNING | MB_DEFBUTTON2) != IDYES) return;

    const PrivilegedLaunchResult result = LaunchPrivilegedServiceAction(action, window_);
    if (!result.launched && result.error != ERROR_CANCELLED) {
        const std::wstring error = std::wstring(localization_.Get(TextId::PrivilegedLaunchFailed)) +
                                   L"\n\nWin32 error: " + std::to_wstring(result.error);
        MessageBoxW(window_, error.c_str(), title.c_str(), MB_OK | MB_ICONERROR);
    }
    pollWake_.notify_all();
}

void App::ToggleMiniHud() {
    ApplyMiniHud(!miniHud_);
}

void App::ApplyMiniHud(bool enabled) {
    if (window_ == nullptr || miniHud_ == enabled) return;

    if (enabled) {
        RECT current{};
        if (GetWindowRect(window_, &current) != FALSE && !IsIconic(window_)) {
            normalRect_ = current;
            hasNormalRect_ = true;
        }
        miniHud_ = true;
        RECT target = normalRect_;
        int x = target.left;
        int y = target.top;
        if (!hasNormalRect_) {
            RECT current{};
            if (GetWindowRect(window_, &current) != FALSE) {
                x = current.left;
                y = current.top;
            }
        }
        SetWindowPos(window_, HWND_TOPMOST, x, y, kMiniWidth, kMiniHeight, SWP_NOACTIVATE | SWP_SHOWWINDOW);
    } else {
        miniHud_ = false;
        if (hasNormalRect_) {
            SetWindowPos(window_, HWND_NOTOPMOST,
                         normalRect_.left, normalRect_.top,
                         normalRect_.right - normalRect_.left,
                         normalRect_.bottom - normalRect_.top,
                         SWP_NOACTIVATE | SWP_SHOWWINDOW);
        } else {
            SetWindowPos(window_, HWND_NOTOPMOST, 0, 0, 1120, 720,
                         SWP_NOMOVE | SWP_NOACTIVATE | SWP_SHOWWINDOW);
        }
    }
    DiscardRenderTarget();
    PersistWindowState();
    InvalidateRect(window_, nullptr, FALSE);
}

void App::PersistWindowState() noexcept {
    if (window_ == nullptr) return;
    RECT rect{};
    if (!miniHud_ && GetWindowRect(window_, &rect) != FALSE && !IsIconic(window_)) {
        normalRect_ = rect;
        hasNormalRect_ = true;
    }
    if (!hasNormalRect_) return;
    WindowState state;
    state.normalRect = normalRect_;
    state.hasNormalRect = true;
    state.miniHud = miniHud_;
    SaveWindowState(state);
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
    PersistWindowState();
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
                bool repaint = false;
                if (visible) {
                    DashboardSnapshot next = client.Poll();
                    {
                        std::lock_guard lock(snapshotMutex_);
                        repaint = !DisplayEquivalent(snapshot_, next);
                        snapshot_ = std::move(next);
                    }
                    if (repaint && window_ != nullptr) PostMessageW(window_, kSnapshotMessage, 0, 0);
                } else {
                    const bool healthy = health.Check();
                    const ServiceState service = QueryAgentServiceState();
                    std::lock_guard lock(snapshotMutex_);
                    DashboardSnapshot next = snapshot_;
                    next.service = service;
                    if (!healthy) next.connection = ConnectionState::Offline;
                    snapshot_ = std::move(next);
                }

                const auto delay = visible ? std::chrono::seconds(2) : std::chrono::seconds(20);
                std::unique_lock waitLock(pollWakeMutex_);
                pollWake_.wait_for(waitLock, delay);
            }
        } catch (...) {
            bool repaint = false;
            {
                std::lock_guard lock(snapshotMutex_);
                DashboardSnapshot next = snapshot_;
                next.connection = ConnectionState::Offline;
                next.error = L"poller initialization failed";
                repaint = !DisplayEquivalent(snapshot_, next);
                snapshot_ = std::move(next);
            }
            if (repaint && window_ != nullptr) PostMessageW(window_, kSnapshotMessage, 0, 0);
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
        case WM_LBUTTONUP: {
            if (miniHud_) break;
            const float x = static_cast<float>(GET_X_LPARAM(lParam));
            const float y = static_cast<float>(GET_Y_LPARAM(lParam));
            if (x >= 0.0f && x <= kSidebarWidth) {
                for (int i = 0; i < 3; ++i) {
                    const float navTop = kNavTop + static_cast<float>(i) * (kNavHeight + kNavGap);
                    if (y >= navTop && y <= navTop + kNavHeight) {
                        page_ = i == 0 ? Page::Dashboard : (i == 1 ? Page::Sources : Page::Diagnostics);
                        InvalidateRect(window_, nullptr, FALSE);
                        return 0;
                    }
                }
            }
            break;
        }
        case WM_KEYDOWN:
            if (wParam == L'M') {
                ToggleMiniHud();
                return 0;
            }
            if (miniHud_) break;
            if (wParam == L'1') page_ = Page::Dashboard;
            else if (wParam == L'2') page_ = Page::Sources;
            else if (wParam == L'3') page_ = Page::Diagnostics;
            else break;
            InvalidateRect(window_, nullptr, FALSE);
            return 0;
        case WM_EXITSIZEMOVE:
            PersistWindowState();
            return 0;
        case WM_SETTINGCHANGE:
            DiscardRenderTarget();
            InvalidateRect(window_, nullptr, FALSE);
            return 0;
        case WM_DPICHANGED: {
            const auto* suggested = reinterpret_cast<RECT*>(lParam);
            SetWindowPos(window_, miniHud_ ? HWND_TOPMOST : nullptr,
                         suggested->left, suggested->top,
                         suggested->right - suggested->left, suggested->bottom - suggested->top,
                         SWP_NOACTIVATE);
            PersistWindowState();
            return 0;
        }
        case WM_GETMINMAXINFO: {
            auto* info = reinterpret_cast<MINMAXINFO*>(lParam);
            info->ptMinTrackSize.x = miniHud_ ? 500 : 900;
            info->ptMinTrackSize.y = miniHud_ ? 190 : 580;
            return 0;
        }
        case kSnapshotMessage:
            if (IsWindowVisible(window_)) InvalidateRect(window_, nullptr, FALSE);
            return 0;
        case kTrayMessage:
            if (LOWORD(lParam) == WM_LBUTTONDBLCLK) ShowDashboard();
            else if (LOWORD(lParam) == WM_RBUTTONUP || LOWORD(lParam) == WM_CONTEXTMENU) {
                POINT point{};
                GetCursorPos(&point);
                ShowTrayMenu(point);
            }
            return 0;
        case WM_COMMAND: {
            const UINT commandId = LOWORD(wParam);
            if (commandId == kTrayOpen) { ShowDashboard(); return 0; }
            if (commandId == kTrayMiniHud) { ToggleMiniHud(); return 0; }
            if (commandId == kTrayExit) { exitRequested_ = true; DestroyWindow(window_); return 0; }
            if (commandId >= kTrayServiceStart && commandId <= kTrayServiceUpgrade) {
                RunPrivilegedCommand(commandId);
                return 0;
            }
            break;
        }
        case WM_CLOSE:
            if (exitRequested_) DestroyWindow(window_); else HideDashboard();
            return 0;
        case WM_DESTROY:
            PersistWindowState();
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
