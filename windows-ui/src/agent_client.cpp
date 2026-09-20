#include "agent_client.h"

#include <winsvc.h>
#include <winrt/Windows.Data.Json.h>
#include <winrt/base.h>

#include <algorithm>
#include <array>
#include <cmath>
#include <optional>
#include <stdexcept>
#include <string_view>

namespace aicontrol::ui {
namespace {

constexpr std::size_t kMaxResponseBytes = 512 * 1024;
constexpr DWORD kTimeoutMs = 2500;

class HttpHandle final {
public:
    explicit HttpHandle(HINTERNET value = nullptr) noexcept : value_(value) {}
    ~HttpHandle() { reset(); }
    HttpHandle(const HttpHandle&) = delete;
    HttpHandle& operator=(const HttpHandle&) = delete;
    HttpHandle(HttpHandle&& other) noexcept : value_(other.release()) {}
    HttpHandle& operator=(HttpHandle&& other) noexcept {
        if (this != &other) {
            reset(other.release());
        }
        return *this;
    }
    [[nodiscard]] HINTERNET get() const noexcept { return value_; }
    [[nodiscard]] HINTERNET release() noexcept {
        HINTERNET value = value_;
        value_ = nullptr;
        return value;
    }
    void reset(HINTERNET value = nullptr) noexcept {
        if (value_ != nullptr) WinHttpCloseHandle(value_);
        value_ = value;
    }

private:
    HINTERNET value_{nullptr};
};

std::wstring Utf8ToWide(std::string_view value) {
    if (value.empty()) return {};
    const int required = MultiByteToWideChar(
        CP_UTF8,
        MB_ERR_INVALID_CHARS,
        value.data(),
        static_cast<int>(value.size()),
        nullptr,
        0
    );
    if (required <= 0) throw std::runtime_error("invalid UTF-8 response");
    std::wstring result(static_cast<std::size_t>(required), L'\0');
    if (MultiByteToWideChar(
            CP_UTF8,
            MB_ERR_INVALID_CHARS,
            value.data(),
            static_cast<int>(value.size()),
            result.data(),
            required) != required) {
        throw std::runtime_error("UTF-8 conversion failed");
    }
    return result;
}

std::wstring HttpError(const wchar_t* prefix, DWORD value) {
    return std::wstring(prefix) + L" " + std::to_wstring(value);
}

using winrt::Windows::Data::Json::JsonArray;
using winrt::Windows::Data::Json::JsonObject;
using winrt::Windows::Data::Json::JsonValueType;

bool HasValue(const JsonObject& object, std::wstring_view name) {
    const winrt::hstring key(name);
    if (!object.HasKey(key)) return false;
    return object.GetNamedValue(key).ValueType() != JsonValueType::Null;
}

std::wstring OptString(const JsonObject& object, std::wstring_view name) {
    if (!HasValue(object, name)) return {};
    const auto value = object.GetNamedValue(winrt::hstring(name));
    if (value.ValueType() != JsonValueType::String) return {};
    return std::wstring(value.GetString());
}

bool OptBool(const JsonObject& object, std::wstring_view name, bool fallback = false) {
    if (!HasValue(object, name)) return fallback;
    const auto value = object.GetNamedValue(winrt::hstring(name));
    if (value.ValueType() != JsonValueType::Boolean) return fallback;
    return value.GetBoolean();
}

std::optional<double> OptNumber(const JsonObject& object, std::wstring_view name) {
    if (!HasValue(object, name)) return std::nullopt;
    const auto value = object.GetNamedValue(winrt::hstring(name));
    if (value.ValueType() != JsonValueType::Number) return std::nullopt;
    const double number = value.GetNumber();
    if (!std::isfinite(number)) return std::nullopt;
    return number;
}

std::optional<std::int64_t> OptInt64(const JsonObject& object, std::wstring_view name) {
    const auto value = OptNumber(object, name);
    if (!value) return std::nullopt;
    return static_cast<std::int64_t>(*value);
}

std::optional<JsonObject> OptObject(const JsonObject& object, std::wstring_view name) {
    if (!HasValue(object, name)) return std::nullopt;
    const auto value = object.GetNamedValue(winrt::hstring(name));
    if (value.ValueType() != JsonValueType::Object) return std::nullopt;
    return value.GetObject();
}

std::optional<JsonArray> OptArray(const JsonObject& object, std::wstring_view name) {
    if (!HasValue(object, name)) return std::nullopt;
    const auto value = object.GetNamedValue(winrt::hstring(name));
    if (value.ValueType() != JsonValueType::Array) return std::nullopt;
    return value.GetArray();
}

void ParseState(const std::string& body, DashboardSnapshot& snapshot) {
    const JsonObject root = JsonObject::Parse(winrt::hstring(Utf8ToWide(body)));
    const auto schema = OptInt64(root, L"schemaVersion");
    if (!schema || *schema != 1) {
        snapshot.connection = ConnectionState::SchemaError;
        snapshot.error = L"schemaVersion";
        return;
    }

    if (auto overall = OptObject(root, L"overall")) {
        snapshot.overallStatus = OptString(*overall, L"status");
    }
    snapshot.connection = snapshot.overallStatus == L"live"
        ? ConnectionState::Live
        : ConnectionState::Degraded;

    if (auto server = OptObject(root, L"server")) {
        snapshot.serverVersion = OptString(*server, L"version");
    }

    if (auto zcode = OptObject(root, L"zcode")) {
        if (auto health = OptObject(*zcode, L"health")) {
            snapshot.zcodeStatus = OptString(*health, L"status");
        }
        if (auto tasks = OptArray(*zcode, L"tasks")) {
            const std::uint32_t count = std::min<std::uint32_t>(tasks->Size(), 32);
            snapshot.tasks.reserve(count);
            for (std::uint32_t i = 0; i < count; ++i) {
                const auto value = tasks->GetAt(i);
                if (value.ValueType() != JsonValueType::Object) continue;
                const JsonObject task = value.GetObject();
                TaskView item;
                item.title = OptString(task, L"title");
                item.workspace = OptString(task, L"workspace");
                item.status = OptString(task, L"status");
                item.activity = OptString(task, L"activity");
                item.durationSeconds = OptInt64(task, L"durationSeconds");
                if (!item.title.empty()) snapshot.tasks.push_back(std::move(item));
            }
        }
    }
    AddConcurrentTaskPreview(snapshot);

    if (auto command = OptObject(root, L"commandCode")) {
        if (auto health = OptObject(*command, L"health")) {
            snapshot.commandCodeStatus = OptString(*health, L"status");
        }
        if (auto usage = OptObject(*command, L"usage")) {
            snapshot.plan = OptString(*usage, L"plan");
            if (auto credit = OptObject(*usage, L"credit")) {
                snapshot.creditRemaining = OptNumber(*credit, L"remaining");
                snapshot.creditLimit = OptNumber(*credit, L"limit");
                snapshot.creditUnit = OptString(*credit, L"unit");
            }
            if (auto windows = OptArray(*usage, L"windows")) {
                const std::uint32_t count = std::min<std::uint32_t>(windows->Size(), 8);
                snapshot.usageWindows.reserve(count);
                for (std::uint32_t i = 0; i < count; ++i) {
                    const auto value = windows->GetAt(i);
                    if (value.ValueType() != JsonValueType::Object) continue;
                    const JsonObject window = value.GetObject();
                    UsageWindowView item;
                    item.name = OptString(window, L"name");
                    item.usedPercent = OptNumber(window, L"usedPercent");
                    item.resetAt = OptString(window, L"resetAt");
                    if (!item.name.empty()) snapshot.usageWindows.push_back(std::move(item));
                }
            }
        }
    }
}

void ParseDiagnostics(const std::string& body, DashboardSnapshot& snapshot) {
    const JsonObject root = JsonObject::Parse(winrt::hstring(Utf8ToWide(body)));
    const auto version = OptInt64(root, L"diagnosticsVersion");
    if (!version || *version != 1) return;
    snapshot.agentVersion = OptString(root, L"version");
    snapshot.uptimeSeconds = OptInt64(root, L"uptimeSeconds");
    if (auto storage = OptObject(root, L"zcodeStorage")) {
        snapshot.zcodeStorage.available = true;
        snapshot.zcodeStorage.bindingMode = OptString(*storage, L"bindingMode");
        snapshot.zcodeStorage.layoutSource = OptString(*storage, L"layoutSource");
        snapshot.zcodeStorage.runtimeDatabaseReadable = OptBool(*storage, L"runtimeDatabaseReadable");
        snapshot.zcodeStorage.taskIndexReadable = OptBool(*storage, L"taskIndexReadable");
        snapshot.zcodeStorage.turnLogReadable = OptBool(*storage, L"turnLogReadable");
        snapshot.zcodeStorage.refreshRecommended = OptBool(*storage, L"refreshRecommended");
    }
    if (auto outbox = OptObject(root, L"outbox")) {
        snapshot.outbox.available = true;
        snapshot.outbox.status = OptString(*outbox, L"status");
        snapshot.outbox.pendingEvents = OptInt64(*outbox, L"pendingEvents").value_or(0);
        snapshot.outbox.taskBaselineRows = OptInt64(*outbox, L"taskBaselineRows").value_or(0);
        snapshot.outbox.compactedTaskRows = OptInt64(*outbox, L"compactedTaskRows").value_or(0);
    }
}

}  // namespace

AgentClient::AgentClient() {
    session_ = WinHttpOpen(
        L"AIControlHUD-WindowsUI/1",
        WINHTTP_ACCESS_TYPE_NO_PROXY,
        WINHTTP_NO_PROXY_NAME,
        WINHTTP_NO_PROXY_BYPASS,
        0
    );
    if (session_ == nullptr) throw std::runtime_error("WinHttpOpen failed");
    WinHttpSetTimeouts(session_, kTimeoutMs, kTimeoutMs, kTimeoutMs, kTimeoutMs);
    connection_ = WinHttpConnect(session_, L"127.0.0.1", 8787, 0);
    if (connection_ == nullptr) {
        WinHttpCloseHandle(session_);
        session_ = nullptr;
        throw std::runtime_error("WinHttpConnect failed");
    }
}

AgentClient::~AgentClient() {
    if (connection_ != nullptr) WinHttpCloseHandle(connection_);
    if (session_ != nullptr) WinHttpCloseHandle(session_);
}

AgentClient::HttpResponse AgentClient::Get(const wchar_t* path) const {
    HttpHandle request(WinHttpOpenRequest(
        connection_,
        L"GET",
        path,
        nullptr,
        WINHTTP_NO_REFERER,
        WINHTTP_DEFAULT_ACCEPT_TYPES,
        0
    ));
    if (request.get() == nullptr) throw std::runtime_error("WinHttpOpenRequest failed");

    const wchar_t* headers = L"Accept: application/json\r\nCache-Control: no-store\r\n";
    if (!WinHttpSendRequest(
            request.get(),
            headers,
            static_cast<DWORD>(-1L),
            WINHTTP_NO_REQUEST_DATA,
            0,
            0,
            0)) {
        throw std::runtime_error("WinHttpSendRequest failed");
    }
    if (!WinHttpReceiveResponse(request.get(), nullptr)) {
        throw std::runtime_error("WinHttpReceiveResponse failed");
    }

    DWORD status = 0;
    DWORD statusSize = sizeof(status);
    if (!WinHttpQueryHeaders(
            request.get(),
            WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER,
            WINHTTP_HEADER_NAME_BY_INDEX,
            &status,
            &statusSize,
            WINHTTP_NO_HEADER_INDEX)) {
        throw std::runtime_error("WinHttpQueryHeaders failed");
    }

    std::string body;
    body.reserve(8192);
    std::array<char, 4096> buffer{};
    for (;;) {
        DWORD read = 0;
        if (!WinHttpReadData(request.get(), buffer.data(), static_cast<DWORD>(buffer.size()), &read)) {
            throw std::runtime_error("WinHttpReadData failed");
        }
        if (read == 0) break;
        if (body.size() + read > kMaxResponseBytes) {
            throw std::runtime_error("Agent response exceeds 512 KiB");
        }
        body.append(buffer.data(), read);
    }
    return HttpResponse{status, std::move(body)};
}

DashboardSnapshot AgentClient::Poll() {
    DashboardSnapshot snapshot;
    snapshot.service = QueryAgentServiceState();
    try {
        const auto state = Get(L"/api/v1/state");
        if (state.status != HTTP_STATUS_OK) {
            snapshot.connection = ConnectionState::ServerError;
            snapshot.error = L"HTTP " + std::to_wstring(state.status);
            return snapshot;
        }
        ParseState(state.body, snapshot);
        if (snapshot.connection == ConnectionState::SchemaError) return snapshot;

        const auto diagnostics = Get(L"/api/v1/diagnostics");
        if (diagnostics.status == HTTP_STATUS_OK) {
            ParseDiagnostics(diagnostics.body, snapshot);
        }
        return snapshot;
    } catch (const winrt::hresult_error& error) {
        snapshot.connection = ConnectionState::ServerError;
        snapshot.error = L"JSON/WinRT " + std::to_wstring(static_cast<unsigned long>(error.code()));
    } catch (const std::exception&) {
        snapshot.connection = ConnectionState::Offline;
        snapshot.error = L"local transport unavailable";
    }
    return snapshot;
}

ServiceState QueryAgentServiceState() noexcept {
    SC_HANDLE manager = OpenSCManagerW(nullptr, nullptr, SC_MANAGER_CONNECT);
    if (manager == nullptr) return ServiceState::Unknown;
    SC_HANDLE service = OpenServiceW(manager, L"AIControlHUD", SERVICE_QUERY_STATUS);
    if (service == nullptr) {
        const DWORD error = GetLastError();
        CloseServiceHandle(manager);
        return error == ERROR_SERVICE_DOES_NOT_EXIST ? ServiceState::NotInstalled : ServiceState::Unknown;
    }

    SERVICE_STATUS_PROCESS status{};
    DWORD bytesNeeded = 0;
    const BOOL ok = QueryServiceStatusEx(
        service,
        SC_STATUS_PROCESS_INFO,
        reinterpret_cast<LPBYTE>(&status),
        sizeof(status),
        &bytesNeeded
    );
    CloseServiceHandle(service);
    CloseServiceHandle(manager);
    if (!ok) return ServiceState::Unknown;

    switch (status.dwCurrentState) {
        case SERVICE_STOPPED: return ServiceState::Stopped;
        case SERVICE_START_PENDING: return ServiceState::StartPending;
        case SERVICE_STOP_PENDING: return ServiceState::StopPending;
        case SERVICE_RUNNING: return ServiceState::Running;
        case SERVICE_PAUSED:
        case SERVICE_PAUSE_PENDING:
        case SERVICE_CONTINUE_PENDING:
            return ServiceState::Paused;
        default: return ServiceState::Unknown;
    }
}

}  // namespace aicontrol::ui
