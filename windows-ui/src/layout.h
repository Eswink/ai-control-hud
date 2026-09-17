#pragma once

#include <algorithm>
#include <cstddef>

namespace aicontrol::ui {

constexpr float kStackedDashboardBreakpoint = 760.0f;

inline bool UseStackedDashboard(float contentWidth) noexcept {
    return contentWidth < kStackedDashboardBreakpoint;
}

inline float StackedTaskCardHeight(float availableHeight) noexcept {
    return std::clamp(availableHeight * 0.46f, 200.0f, 250.0f);
}

inline std::size_t CommandUsageRowCount(float cardHeight) noexcept {
    if (cardHeight >= 250.0f) return 2;
    if (cardHeight >= 205.0f) return 1;
    return 0;
}

}  // namespace aicontrol::ui
