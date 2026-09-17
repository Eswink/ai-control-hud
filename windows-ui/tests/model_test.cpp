#include "model.h"

#include <cassert>
#include <cmath>

using namespace aicontrol::ui;

int main() {
    DashboardSnapshot snapshot;
    snapshot.tasks = {
        TaskView{L"done", L"repo", L"completed", L"", 10},
        TaskView{L"wait", L"repo", L"waiting", L"", 20},
        TaskView{L"run", L"repo", L"running", L"build", 30},
    };
    const TaskView* current = CurrentTask(snapshot);
    assert(current != nullptr);
    assert(current->title == L"run");

    snapshot.creditRemaining = 25.0;
    snapshot.creditLimit = 100.0;
    const auto percent = CreditRemainingPercent(snapshot);
    assert(percent.has_value());
    assert(std::abs(*percent - 25.0) < 0.0001);

    snapshot.creditRemaining = 120.0;
    snapshot.creditLimit = 100.0;
    assert(CreditRemainingPercent(snapshot).value() == 100.0);

    snapshot.creditLimit = 0.0;
    assert(!CreditRemainingPercent(snapshot).has_value());

    DashboardSnapshot empty;
    assert(CurrentTask(empty) == nullptr);
    assert(!CreditRemainingPercent(empty).has_value());
    return 0;
}
