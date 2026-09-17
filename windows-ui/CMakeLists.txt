cmake_minimum_required(VERSION 3.25)
project(ai_control_agent_ui LANGUAGES CXX)

add_executable(ai-control-agent-ui WIN32
    src/main.cpp
    src/app.cpp
    src/agent_client.cpp
    src/localization.cpp
    src/privileged_actions.cpp
    src/window_state.cpp
)

target_compile_features(ai-control-agent-ui PRIVATE cxx_std_20)
target_compile_definitions(ai-control-agent-ui PRIVATE
    UNICODE
    _UNICODE
    WIN32_LEAN_AND_MEAN
    NOMINMAX
)

target_include_directories(ai-control-agent-ui PRIVATE src)

target_link_libraries(ai-control-agent-ui PRIVATE
    comdlg32
    d2d1
    dwrite
    dwmapi
    ole32
    shell32
    winhttp
    windowsapp
)

if(MSVC)
    target_compile_options(ai-control-agent-ui PRIVATE /W4 /WX /permissive- /EHsc /utf-8)
    target_link_options(ai-control-agent-ui PRIVATE /Brepro)
    # Keep /WX globally. Suppress only C4456 in app.cpp, where Mini HUD geometry
    # intentionally uses nested RECT scratch variables with independent lifetimes.
    set_source_files_properties(src/app.cpp PROPERTIES COMPILE_OPTIONS "/wd4456")
    set_property(TARGET ai-control-agent-ui PROPERTY
        MSVC_RUNTIME_LIBRARY "MultiThreaded$<$<CONFIG:Debug>:Debug>")
endif()

add_executable(ai-control-agent-ui-model-test tests/model_test.cpp)
target_compile_features(ai-control-agent-ui-model-test PRIVATE cxx_std_20)
target_include_directories(ai-control-agent-ui-model-test PRIVATE src)
if(MSVC)
    target_compile_options(ai-control-agent-ui-model-test PRIVATE /W4 /WX /permissive- /EHsc /utf-8)
    target_link_options(ai-control-agent-ui-model-test PRIVATE /Brepro)
    set_property(TARGET ai-control-agent-ui-model-test PROPERTY
        MSVC_RUNTIME_LIBRARY "MultiThreaded$<$<CONFIG:Debug>:Debug>")
endif()

enable_testing()
add_test(NAME windows-ui-model COMMAND ai-control-agent-ui-model-test)
