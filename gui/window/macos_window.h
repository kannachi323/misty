#pragma once


#include "window.h"

class MacOSWindow : public Window {
public:
    MacOSWindow(unsigned int width, unsigned int height, const std::string& title);
    ~MacOSWindow() override;

    void init_platform_api() const override;
    bool should_close() const override;
    void prepare_frame() const override;
    void present_frame() const override;
    

protected:



private:

};