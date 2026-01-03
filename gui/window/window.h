#pragma once
#include <string>
#include <utility>
#include <GLFW/glfw3.h>

class Window {
public:
    Window(unsigned int width, unsigned int height, const std::string& title);
    ~Window();

    void init_platform_api() const;

    bool should_close() const;
    void prepare_frame() const;
    void present_frame() const;
   

private:
    GLFWwindow* window_;
    
};