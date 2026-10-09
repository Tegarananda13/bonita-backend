package routes

import (
	"bonita-backend/controllers"
	"bonita-backend/middleware"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine) {

	// ======================
	// LOGIN
	// ======================
	r.POST(
		"/login",
		controllers.Login,
	)

	// ======================
	// PUBLIC (CUSTOMER)
	// ======================
	r.GET(
		"/paket",
		controllers.GetPaket,
	)

	r.GET(
		"/paket/:id",
		controllers.GetDetailPaket,
	)

	r.POST(
		"/pendaftaran",
		controllers.CreatePendaftaran,
	)
	// r.GET("/pendaftaran/:nomor", controllers.GetPendaftaranByNomor)

	r.POST(
		"/otp/request",
		controllers.RequestOTP,
	)
	r.POST(
		"/otp/verify",
		controllers.VerifyOTP,
	)

	r.POST(
		"/chatbot",
		controllers.Chatbot,
	)

	// hanya lihat status

	// ======================
	// CUSTOMER SESSION (OTP)
	// ======================

	customer := r.Group("/customer")
	customer.Use(
		middleware.CustomerMiddleware(),
	)

	{
		customer.POST(
			"/pembayaran",
			controllers.CreatePembayaran,
		)

		customer.POST(
			"/pembayaran/:id/upload",
			controllers.UploadBuktiPembayaran,
		)

		customer.GET(
			"/pembayaran",
			controllers.GetPembayaran,
		)

		customer.GET(
			"/dashboard",
			controllers.GetCustomerDashboard,
		)

		customer.POST(
			"/dokumen/upload",
			controllers.UploadDokumen,
		)

		customer.GET(
			"/dokumen",
			controllers.GetDokumen,
		)

		customer.GET(
			"/invoice",
			controllers.GetInvoice,
		)

		customer.GET(
			"/status",
			controllers.GetCustomerPendaftaranStatus,
		)
	}
	// ======================
	// ADMIN & ADMINISTRATION MANAGER
	// ======================

	admin := r.Group("/admin")
	admin.Use(
		middleware.AuthMiddleware(),
		middleware.RoleMiddleware("admin", "owner", "manager", "administration_manager"),
	)

	{
		admin.GET(
			"/me",
			controllers.GetMe,
		)

		admin.GET(
			"/dashboard",
			controllers.GetDashboard,
		)

		// ── Badges notifikasi admin ──
		admin.GET(
			"/badges",
			controllers.GetAdminBadges,
		)
		admin.GET(
			"/notifikasi/count",
			controllers.GetAdminBadges,
		)
		admin.GET(
			"/dokumen/count",
			controllers.GetDokumenPendingCount,
		)
		admin.GET(
			"/pembayaran/count",
			controllers.GetPembayaranPendingCount,
		)

		// ── Customer (admin mendaftarkan jamaah) ──
		admin.GET(
			"/customer",
			controllers.AdminGetAllCustomer,
		)

		admin.POST(
			"/customer",
			controllers.AdminCreateCustomer,
		)

		admin.GET(
			"/pendaftaran",
			controllers.GetAllPendaftaran,
		)

		admin.GET(
			"/pendaftaran/saya",
			controllers.GetPendaftaranSaya,
		)

		admin.PUT(
			"/pendaftaran/:nomor/assign",
			controllers.AssignPendaftaran,
		)

		admin.PUT(
			"/pendaftaran/:nomor/selesai",
			controllers.TandaiSelesai,
		)

		admin.POST(
			"/pendaftaran/process-expiry",
			controllers.ManualProcessExpiry,
		)

		admin.GET(
			"/pendaftaran/:nomor",
			controllers.GetDetailPendaftaran,
		)

		admin.GET(
			"/pendaftaran/:nomor/status",
			controllers.GetPendaftaranStatus,
		)

		admin.GET(
			"/pembayaran/pending",
			controllers.GetPendingPembayaran,
		)

		admin.PUT(
			"/pembayaran/:id/verifikasi",
			controllers.VerifikasiPembayaran,
		)

		admin.GET(
			"/pembayaran/:id",
			controllers.GetDetailPembayaran,
		)

		admin.GET(
			"/dokumen/pending",
			controllers.GetPendingDokumen,
		)

		admin.PUT(
			"/dokumen/:id/verifikasi",
			controllers.VerifikasiDokumen,
		)

		admin.GET(
			"/dokumen/:id",
			controllers.GetDetailDokumen,
		)

		admin.POST(
			"/paket",
			controllers.CreatePaket,
		)

		admin.POST(
			"/paket/:id/fasilitas",
			controllers.CreateFasilitas,
		)

		admin.GET(
			"/paket/:id/fasilitas",
			controllers.GetFasilitasByPaket,
		)

		admin.PUT(
			"/fasilitas/:id",
			controllers.UpdateFasilitas,
		)

		admin.DELETE(
			"/fasilitas/:id",
			controllers.DeleteFasilitas,
		)

		admin.PUT(
			"/paket/:id",
			controllers.UpdatePaket,
		)

		admin.GET(
			"/paket",
			controllers.GetAllPaket,
		)

		admin.GET(
			"/paket/:id",
			controllers.GetPaketByID,
		)

		admin.GET(
			"/paket/:id/detail",
			controllers.GetDetailPaketAdmin,
		)

		admin.DELETE(
			"/paket/:id",
			controllers.DeletePaket,
		)

		admin.PATCH(
			"/paket/:id/status",
			controllers.ToggleStatusPaket,
		)

		admin.PATCH(
			"/paket/:id/finish",
			controllers.FinishPaket,
		)

		// ── Foto Paket ──
		admin.POST("/paket/:id/foto", controllers.UploadFotoPaket)
		admin.GET("/paket/:id/foto", controllers.GetFotoPaket)
		admin.DELETE("/foto-paket/:id", controllers.DeleteFotoPaket)
		admin.PATCH("/foto-paket/:id/utama", controllers.SetFotoUtama)

		// ── Foto Fasilitas ──
		admin.POST("/fasilitas/:id/foto", controllers.UploadFotoFasilitas)
		admin.GET("/fasilitas/:id/foto", controllers.GetFotoFasilitas)
		admin.DELETE("/foto-fasilitas/:id", controllers.DeleteFotoFasilitas)

		// ── Pengaduan ──
		admin.GET("/pengaduan", controllers.GetAllPengaduan)
		admin.GET("/pengaduan/:id", controllers.GetDetailPengaduan)
		admin.PATCH("/pengaduan/:id/status", controllers.UpdateStatusPengaduan)
		admin.GET("/invoice", controllers.GetInvoiceAdmin)

		// ── Admin input pembayaran & dokumen dari halaman detail ──
		admin.POST("/pendaftaran/:nomor/pembayaran", controllers.AdminCreatePembayaran)
		admin.PUT("/pembayaran/:id/admin", controllers.AdminUpdatePembayaran)
		admin.DELETE("/pembayaran/:id/admin", controllers.AdminDeletePembayaran)

		admin.POST("/pendaftaran/:nomor/dokumen", controllers.AdminUploadDokumen)
		admin.PUT("/dokumen/:id/admin", controllers.AdminUpdateDokumen)
		admin.DELETE("/dokumen/:id/admin", controllers.AdminDeleteDokumen)

		// ── Ajukan Verifikasi ke Manager setelah perbaikan ──
		admin.POST("/pendaftaran/:nomor/ajukan-verifikasi", controllers.AdminAjukanVerifikasi)
	}

	// ======================
	// ADMINISTRATION MANAGER
	// ======================

	manager := r.Group("/manager")
	manager.Use(
		middleware.AuthMiddleware(),
		middleware.RoleMiddleware("owner", "manager", "administration_manager"),
	)
	{
		// Manajemen Akun Admin
		manager.POST("/admin", controllers.CreateAdmin)
		manager.GET("/admin", controllers.GetAdminList)
		manager.GET("/admin/:id", controllers.GetAdminDetail)
		manager.PUT("/admin/:id", controllers.UpdateAdmin)
		manager.PATCH("/admin/:id/deactivate", controllers.DeactivateAdmin)
		manager.PATCH("/admin/:id/reactivate", controllers.ReactivateAdmin)
		manager.DELETE("/admin/:id", controllers.DeleteAdmin)

		// Laporan & Rekapitulasi
		manager.GET("/laporan", controllers.GetLaporan)

		// Verifikasi Final Pendaftaran
		manager.GET("/verifikasi", controllers.GetManagerQueue)
		manager.GET("/verifikasi/:nomor", controllers.GetManagerDetailVerifikasi)
		manager.POST("/verifikasi/:nomor/approve", controllers.ManagerApprovePendaftaran)
		manager.POST("/verifikasi/:nomor/perbaikan", controllers.ManagerMintaPerbaikan)

		// Dokumen Perjalanan (Visa, Tiket, Nusuk)
		manager.GET("/dokumen-perjalanan/:nomor", controllers.GetDokumenPerjalanan)
		manager.POST("/pendaftaran/:nomor/dokumen-perjalanan", controllers.ManagerUploadDokumenPerjalanan)
		manager.DELETE("/dokumen-perjalanan/:id", controllers.ManagerDeleteDokumenPerjalanan)

		// Badges Manager
		manager.GET("/badges", controllers.GetManagerBadges)
	}
}
