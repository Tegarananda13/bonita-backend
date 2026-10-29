package routes

import (
	"bonita-backend/controllers/admin"
	"bonita-backend/controllers/administration_manager"
	"bonita-backend/controllers/customer"
	"bonita-backend/controllers/shared"
	"bonita-backend/middleware"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine) {

	// ======================
	// LOGIN (SHARED)
	// ======================
	r.POST(
		"/login",
		shared.Login,
	)

	// ======================
	// PUBLIC / SHARED
	// ======================
	r.GET(
		"/paket",
		shared.GetPaket,
	)

	r.GET(
		"/paket/:id",
		shared.GetDetailPaket,
	)

	r.POST(
		"/pendaftaran",
		customer.CreatePendaftaran,
	)

	r.POST(
		"/otp/request",
		shared.RequestOTP,
	)
	r.POST(
		"/otp/verify",
		shared.VerifyOTP,
	)

	r.POST(
		"/chatbot",
		shared.Chatbot,
	)

	r.GET(
		"/ulasan",
		shared.GetPublicUlasan,
	)

	// ======================
	// CUSTOMER SESSION (OTP)
	// ======================

	custGroup := r.Group("/customer")
	custGroup.Use(
		middleware.CustomerMiddleware(),
	)

	{
		custGroup.POST(
			"/pembayaran",
			customer.CreatePembayaran,
		)

		custGroup.POST(
			"/pembayaran/:id/upload",
			customer.UploadBuktiPembayaran,
		)

		custGroup.GET(
			"/pembayaran",
			customer.GetPembayaran,
		)

		custGroup.GET(
			"/dashboard",
			customer.GetCustomerDashboard,
		)

		custGroup.PUT(
			"/jamaah",
			customer.UpdateCustomerJamaah,
		)

		custGroup.POST(
			"/dokumen/upload",
			customer.UploadDokumen,
		)

		custGroup.GET(
			"/dokumen",
			customer.GetDokumen,
		)

		custGroup.GET(
			"/invoice",
			customer.GetInvoice,
		)

		custGroup.GET(
			"/status",
			customer.GetCustomerPendaftaranStatus,
		)

		custGroup.POST(
			"/ulasan",
			customer.CreateCustomerUlasan,
		)

		custGroup.GET(
			"/ulasan",
			customer.GetCustomerMyUlasan,
		)
	}

	// ======================
	// ADMIN & ADMINISTRATION MANAGER
	// ======================

	admGroup := r.Group("/admin")
	admGroup.Use(
		middleware.AuthMiddleware(),
		middleware.RoleMiddleware("admin", "owner", "manager", "administration_manager"),
	)

	{
		admGroup.GET(
			"/me",
			shared.GetMe,
		)

		admGroup.GET(
			"/dashboard",
			admin.GetDashboard,
		)

		// ── Badges notifikasi admin ──
		admGroup.GET(
			"/badges",
			admin.GetAdminBadges,
		)
		admGroup.GET(
			"/notifikasi/count",
			admin.GetAdminBadges,
		)
		admGroup.GET(
			"/dokumen/count",
			admin.GetDokumenPendingCount,
		)
		admGroup.GET(
			"/pembayaran/count",
			admin.GetPembayaranPendingCount,
		)

		// ── Customer (admin mendaftarkan jamaah) ──
		admGroup.GET(
			"/customer",
			admin.AdminGetAllCustomer,
		)

		admGroup.POST(
			"/customer",
			admin.AdminCreateCustomer,
		)

		admGroup.GET(
			"/pendaftaran",
			admin.GetAllPendaftaran,
		)

		admGroup.GET(
			"/pendaftaran/saya",
			admin.GetPendaftaranSaya,
		)

		admGroup.PUT(
			"/pendaftaran/:nomor/assign",
			admin.AssignPendaftaran,
		)

		admGroup.PUT(
			"/pendaftaran/:nomor/selesai",
			admin.TandaiSelesai,
		)

		admGroup.POST(
			"/pendaftaran/process-expiry",
			admin.ManualProcessExpiry,
		)

		admGroup.GET(
			"/pendaftaran/:nomor",
			admin.GetDetailPendaftaran,
		)

		admGroup.PUT(
			"/pendaftaran/:nomor/jamaah",
			admin.AdminUpdateJamaah,
		)

		admGroup.POST(
			"/pendaftaran/:nomor/batal",
			admin.AdminBatalkanPendaftaran,
		)

		admGroup.PUT(
			"/pendaftaran/:nomor/batal",
			admin.AdminBatalkanPendaftaran,
		)

		admGroup.PUT(
			"/pendaftaran/:nomor/perlengkapan",
			admin.AdminUpdatePerlengkapan,
		)

		admGroup.GET(
			"/perlengkapan/master",
			admin.GetPerlengkapanMaster,
		)

		admGroup.GET(
			"/pendaftaran/:nomor/status",
			admin.GetPendaftaranStatus,
		)

		admGroup.GET(
			"/pembayaran/pending",
			admin.GetPendingPembayaran,
		)

		admGroup.PUT(
			"/pembayaran/:id/verifikasi",
			admin.VerifikasiPembayaran,
		)

		admGroup.GET(
			"/pembayaran/:id",
			admin.GetDetailPembayaran,
		)

		admGroup.GET(
			"/dokumen/pending",
			admin.GetPendingDokumen,
		)

		admGroup.PUT(
			"/dokumen/:id/verifikasi",
			admin.VerifikasiDokumen,
		)

		admGroup.GET(
			"/dokumen/:id",
			admin.GetDetailDokumen,
		)

		admGroup.POST(
			"/paket",
			admin.CreatePaket,
		)

		admGroup.POST(
			"/paket/:id/fasilitas",
			admin.CreateFasilitas,
		)

		admGroup.GET(
			"/paket/:id/fasilitas",
			admin.GetFasilitasByPaket,
		)

		admGroup.PUT(
			"/fasilitas/:id",
			admin.UpdateFasilitas,
		)

		admGroup.DELETE(
			"/fasilitas/:id",
			admin.DeleteFasilitas,
		)

		admGroup.PUT(
			"/paket/:id",
			admin.UpdatePaket,
		)

		admGroup.GET(
			"/paket",
			admin.GetAllPaket,
		)

		admGroup.GET(
			"/paket/:id",
			admin.GetPaketByID,
		)

		admGroup.GET(
			"/paket/:id/detail",
			admin.GetDetailPaketAdmin,
		)

		admGroup.DELETE(
			"/paket/:id",
			admin.DeletePaket,
		)

		admGroup.PATCH(
			"/paket/:id/status",
			admin.ToggleStatusPaket,
		)

		admGroup.PATCH(
			"/paket/:id/finish",
			admin.FinishPaket,
		)

		// ── Foto Paket ──
		admGroup.POST("/paket/:id/foto", admin.UploadFotoPaket)
		admGroup.GET("/paket/:id/foto", admin.GetFotoPaket)
		admGroup.DELETE("/foto-paket/:id", admin.DeleteFotoPaket)
		admGroup.PATCH("/foto-paket/:id/utama", admin.SetFotoUtama)

		// ── Foto Fasilitas ──
		admGroup.POST("/fasilitas/:id/foto", admin.UploadFotoFasilitas)
		admGroup.GET("/fasilitas/:id/foto", admin.GetFotoFasilitas)
		admGroup.DELETE("/foto-fasilitas/:id", admin.DeleteFotoFasilitas)

		// ── Pengaduan ──
		admGroup.GET("/pengaduan", admin.GetAllPengaduan)
		admGroup.GET("/pengaduan/:id", admin.GetDetailPengaduan)
		admGroup.PATCH("/pengaduan/:id/status", admin.UpdateStatusPengaduan)
		admGroup.GET("/invoice", admin.GetInvoiceAdmin)

		// ── Admin input pembayaran & dokumen dari halaman detail ──
		admGroup.POST("/pendaftaran/:nomor/pembayaran", admin.AdminCreatePembayaran)
		admGroup.PUT("/pembayaran/:id/admin", admin.AdminUpdatePembayaran)
		admGroup.DELETE("/pembayaran/:id/admin", admin.AdminDeletePembayaran)

		admGroup.POST("/pendaftaran/:nomor/dokumen", admin.AdminUploadDokumen)
		admGroup.PUT("/dokumen/:id/admin", admin.AdminUpdateDokumen)
		admGroup.DELETE("/dokumen/:id/admin", admin.AdminDeleteDokumen)

		// ── Ajukan Verifikasi ke Manager setelah perbaikan ──
		admGroup.POST("/pendaftaran/:nomor/ajukan-verifikasi", admin.AdminAjukanVerifikasi)

		// ── Ulasan Jamaah ──
		admGroup.GET("/ulasan", admin.AdminGetAllUlasan)
		admGroup.GET("/ulasan/:id", admin.AdminGetDetailUlasan)
		admGroup.PUT("/ulasan/:id/status", admin.AdminUpdateStatusUlasan)
		admGroup.PATCH("/ulasan/:id/status", admin.AdminUpdateStatusUlasan)
		admGroup.DELETE("/ulasan/:id", admin.AdminDeleteUlasan)
	}

	// ======================
	// ADMINISTRATION MANAGER
	// ======================

	mgrGroup := r.Group("/manager")
	mgrGroup.Use(
		middleware.AuthMiddleware(),
		middleware.RoleMiddleware("owner", "manager", "administration_manager"),
	)
	{
		// Manajemen Akun Admin
		mgrGroup.POST("/admin", administration_manager.CreateAdmin)
		mgrGroup.GET("/admin", administration_manager.GetAdminList)
		mgrGroup.GET("/admin/:id", administration_manager.GetAdminDetail)
		mgrGroup.GET("/admin/:id/jamaah", administration_manager.GetAdminJamaah)
		mgrGroup.PUT("/admin/:id", administration_manager.UpdateAdmin)
		mgrGroup.PATCH("/admin/:id/deactivate", administration_manager.DeactivateAdmin)
		mgrGroup.PATCH("/admin/:id/reactivate", administration_manager.ReactivateAdmin)
		mgrGroup.DELETE("/admin/:id", administration_manager.DeleteAdmin)

		// Laporan & Rekapitulasi
		mgrGroup.GET("/laporan", administration_manager.GetLaporan)

		// Verifikasi Final Pendaftaran
		mgrGroup.GET("/verifikasi", administration_manager.GetManagerQueue)
		mgrGroup.GET("/verifikasi/:nomor", administration_manager.GetManagerDetailVerifikasi)
		mgrGroup.POST("/verifikasi/:nomor/approve", administration_manager.ManagerApprovePendaftaran)
		mgrGroup.POST("/verifikasi/:nomor/perbaikan", administration_manager.ManagerMintaPerbaikan)

		// Dokumen Perjalanan (Visa, Tiket, Nusuk)
		mgrGroup.GET("/dokumen-perjalanan/:nomor", administration_manager.GetDokumenPerjalanan)
		mgrGroup.POST("/pendaftaran/:nomor/dokumen-perjalanan", administration_manager.ManagerUploadDokumenPerjalanan)
		mgrGroup.DELETE("/dokumen-perjalanan/:id", administration_manager.ManagerDeleteDokumenPerjalanan)

		// Badges Manager
		mgrGroup.GET("/badges", administration_manager.GetManagerBadges)
	}
}
