#include "model.h"

#include <cmath>

using namespace aicontrol::ui;

namespace {

bool Close(double left, double right) {
    return std::abs(left - right) < 0.0001;
}

}  // namespace

int main() {
    DashboardSnapshot snapshot;
    snapshot.tasks = {
        TaskView{L"done", L"repo", L"completed", L"", 10},
        TaskView{L"wait", L"repo", L"waiting", L"", 20},
        TaskView{L"run", L"repo", L"running", L"build", 30},
    };
    const TaskView* current = CurrentTask(snapshot);
    if (current == nullptr || current->title != L"run") return 1;

    snapshot.creditRemaining = 25.0;
    snapshot.creditLimit = 100.0;
    const auto percent = CreditRemainingPercent(snapshot);
    if (!percent || !Close(*percent, 25.0)) return 2;

    snapshot.creditRemaining = 120.0;
    snapshot.creditLimit = 100.0;
    const auto capped = CreditRemainingPercent(snapshot);
    if (!capped || !Close(*capped, 100.0)) return 3;

    snapshot.creditLimit = 0.0;
    if (CreditRemainingPercent(snapshot).has_value()) return 4;

    DashboardSnapshot empty;
    if (CurrentTask(empty) != nullptr) return 5;
    if (CreditRemainingPercent(empty).has_value()) return 6;

    DashboardSnapshot left;
    left.connection = ConnectionState::Live;
    left.plan = L"pro";
    left.creditRemaining = 42.0;
    left.tasks = {TaskView{L"task", L"repo", L"running", L"build", 5}};
    left.uptimeSeconds = 100;

    DashboardSnapshot right = left;
    right.uptimeSeconds = 102;
    right.tasks.front().durationSeconds = 7;
    if (!DisplayEquivalent(left, right)) return 7;

    right.tasks.front().status = L"completed";
    if (DisplayEquivalent(left, right)) return 8;
    right = left;
    right.creditRemaining = 41.0;
    if (DisplayEquivalent(left, right)) return 9;
    right = left;
    right.outbox.pendingEvents = 1;
    if (DisplayEquivalent(left, right)) return 10;
    return 0;
}
