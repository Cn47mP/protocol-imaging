package protocolimaging

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册协议映射全景图工具的 Custom Action
func Register() {
	maa.AgentServerRegisterCustomAction("ProtocolImagingCaptureGrid", &CaptureGridAction{})
	maa.AgentServerRegisterCustomAction("ProtocolImagingStitch", &StitchAction{})
}
