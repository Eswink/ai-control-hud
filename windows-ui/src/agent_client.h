#pragma once

#include "model.h"

#include <Windows.h>
#include <winhttp.h>

#include <string>

namespace aicontrol::ui {

class AgentClient final {
public:
    AgentClient();
    ~AgentClient();

    AgentClient(const AgentClient&) = delete;
    AgentClient& operator=(const AgentClient&) = delete;

    [[nodiscard]] DashboardSnapshot Poll();

private:
    struct HttpResponse {
        DWORD status{0};
        std::string body;
    };

    [[nodiscard]] HttpResponse Get(const wchar_t* path) const;

    HINTERNET session_{nullptr};
    HINTERNET connection_{nullptr};
};

[[nodiscard]] ServiceState QueryAgentServiceState() noexcept;

}  // namespace aicontrol::ui
