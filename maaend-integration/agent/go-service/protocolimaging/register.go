package protocolimaging

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册协议映射全景图工具的扩展动作
func Register() {
    maa.AgentServerRegisterCustomAction(protocolImagingCaptureActionName, &ProtocolImagingCaptureAction{})
    maa.AgentServerRegisterCustomAction(protocolImagingStitchActionName, &ProtocolImagingStitchAction{})
}
