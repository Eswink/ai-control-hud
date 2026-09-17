#pragma once

#include <Windows.h>
#include <winhttp.h>

namespace aicontrol::ui {

class HealthProbe final {
public:
    HealthProbe() noexcept {
        session_ = WinHttpOpen(
            L"AIControlHUD-WindowsUI-Health/1",
            WINHTTP_ACCESS_TYPE_NO_PROXY,
            WINHTTP_NO_PROXY_NAME,
            WINHTTP_NO_PROXY_BYPASS,
            0
        );
        if (session_ == nullptr) return;
        WinHttpSetTimeouts(session_, 1000, 1000, 1000, 1000);
        connection_ = WinHttpConnect(session_, L"127.0.0.1", 8787, 0);
        if (connection_ == nullptr) {
            WinHttpCloseHandle(session_);
            session_ = nullptr;
        }
    }

    ~HealthProbe() {
        if (connection_ != nullptr) WinHttpCloseHandle(connection_);
        if (session_ != nullptr) WinHttpCloseHandle(session_);
    }

    HealthProbe(const HealthProbe&) = delete;
    HealthProbe& operator=(const HealthProbe&) = delete;

    [[nodiscard]] bool Check() const noexcept {
        if (connection_ == nullptr) return false;
        HINTERNET request = WinHttpOpenRequest(
            connection_,
            L"GET",
            L"/api/v1/health",
            nullptr,
            WINHTTP_NO_REFERER,
            WINHTTP_DEFAULT_ACCEPT_TYPES,
            WINHTTP_FLAG_REFRESH
        );
        if (request == nullptr) return false;

        const BOOL sent = WinHttpSendRequest(
            request,
            WINHTTP_NO_ADDITIONAL_HEADERS,
            0,
            WINHTTP_NO_REQUEST_DATA,
            0,
            0,
            0
        );
        const BOOL received = sent ? WinHttpReceiveResponse(request, nullptr) : FALSE;
        DWORD status = 0;
        DWORD size = sizeof(status);
        const BOOL queried = received ? WinHttpQueryHeaders(
            request,
            WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER,
            WINHTTP_HEADER_NAME_BY_INDEX,
            &status,
            &size,
            WINHTTP_NO_HEADER_INDEX
        ) : FALSE;
        WinHttpCloseHandle(request);
        return queried && status == HTTP_STATUS_OK;
    }

private:
    HINTERNET session_{nullptr};
    HINTERNET connection_{nullptr};
};

}  // namespace aicontrol::ui
