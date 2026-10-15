import AVFoundation
import SwiftUI
import UIKit
import VisionKit

/// 扫码配对: the camera, looking for the QR code on the computer's sign-in page. Shown as a full-screen
/// cover from 接入 AI 助手; reading a code for this server closes it and hands the code back.
/// (Every iPhone that runs iOS 18 has the Neural Engine DataScanner needs; it is unavailable only
/// without camera access, so there is no AVFoundation fallback.)
struct PairScanner: View {
    let onCode: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(Session.self) private var session
    @State private var access = AVCaptureDevice.authorizationStatus(for: .video)
    @State private var problem: String?
    @State private var read = 0

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if access == .authorized, DataScannerViewController.isSupported, DataScannerViewController.isAvailable {
                QRScanner(onPayload: handle).ignoresSafeArea()
            } else if access == .notDetermined {
                ProgressView().tint(.white)
            } else {
                unavailable
            }
        }
        .overlay(alignment: .topTrailing) {
            Button { dismiss() } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(width: 40, height: 40)
                    .background(.black.opacity(0.45), in: Circle())
            }
            .padding(.trailing, 16).padding(.top, 8)
            .accessibilityLabel("关闭")
            .accessibilityIdentifier("pair-scan-close")
        }
        .overlay(alignment: .bottom) {
            Text(problem ?? "对准电脑浏览器里的配对二维码")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(problem == nil ? .white : Color.danger.wash(0.25))
                .multilineTextAlignment(.center)
                .padding(.horizontal, 18).padding(.vertical, 12)
                .background(.black.opacity(0.6), in: RoundedRectangle(cornerRadius: 14))
                .padding(.horizontal, 24).padding(.bottom, 28)
                .accessibilityIdentifier("pair-scan-hint")
        }
        .sensoryFeedback(.success, trigger: read)
        .task {
            guard access == .notDetermined else { return }
            _ = await AVCaptureDevice.requestAccess(for: .video)
            access = AVCaptureDevice.authorizationStatus(for: .video)
        }
    }

    private var unavailable: some View {
        VStack(spacing: 14) {
            Image(systemName: "camera.fill").font(.system(size: 40)).foregroundStyle(.white.opacity(0.6))
            if access == .denied || access == .restricted {
                Text("没有相机权限").font(.headline).foregroundStyle(.white)
                Button("去设置里打开") {
                    if let url = URL(string: UIApplication.openSettingsURLString) { UIApplication.shared.open(url) }
                }
                .buttonStyle(.wide(.tinted, small: true))
                .frame(width: 180)
            } else {
                Text("这台设备不能扫码，请输入配对码").font(.headline).foregroundStyle(.white)
            }
        }
        .padding(.horizontal, 32)
    }

    private func handle(_ payload: String) {
        guard let link = PairLink(payload) else {
            problem = "这不是 noted 的配对二维码"
            return
        }
        if let why = session.pairingMismatch(link.server) {
            problem = why
            return
        }
        read += 1
        onCode(link.code)
        dismiss()
    }
}

/// VisionKit's live scanner, QR codes only.
private struct QRScanner: UIViewControllerRepresentable {
    let onPayload: (String) -> Void

    func makeUIViewController(context: Context) -> DataScannerViewController {
        let vc = DataScannerViewController(recognizedDataTypes: [.barcode(symbologies: [.qr])],
                                           qualityLevel: .balanced,
                                           recognizesMultipleItems: false,
                                           isHighFrameRateTrackingEnabled: false,
                                           isGuidanceEnabled: false,
                                           isHighlightingEnabled: true)
        vc.delegate = context.coordinator
        return vc
    }

    func updateUIViewController(_ vc: DataScannerViewController, context: Context) {
        context.coordinator.onPayload = onPayload
        if !vc.isScanning { try? vc.startScanning() }
    }

    static func dismantleUIViewController(_ vc: DataScannerViewController, coordinator: Coordinator) {
        vc.stopScanning()
    }

    func makeCoordinator() -> Coordinator { Coordinator(onPayload: onPayload) }

    @MainActor
    final class Coordinator: NSObject, DataScannerViewControllerDelegate {
        var onPayload: (String) -> Void
        init(onPayload: @escaping (String) -> Void) { self.onPayload = onPayload }

        func dataScanner(_ dataScanner: DataScannerViewController, didAdd addedItems: [RecognizedItem], allItems: [RecognizedItem]) {
            for item in addedItems {
                if case .barcode(let code) = item, let s = code.payloadStringValue {
                    onPayload(s)
                    return
                }
            }
        }

        func dataScanner(_ dataScanner: DataScannerViewController, didTapOn item: RecognizedItem) {
            if case .barcode(let code) = item, let s = code.payloadStringValue { onPayload(s) }
        }
    }
}
