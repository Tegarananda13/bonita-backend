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
	r.POST("/login", controllers.Login)

	// ======================
	// PUBLIC (CUSTOMER)
	// ======================

	r.POST("/pendaftaran", controllers.CreatePendaftaran)
	r.GET("/pendaftaran/:nomor", controllers.GetPendaftaranByNomor)

	r.POST("/otp/request", controllers.RequestOTP)
	r.POST("/otp/verify", controllers.VerifyOTP)

	// hanya lihat status
	r.GET("/pembayaran/:nomor", controllers.GetPembayaranByNomor)
	r.GET("/dokumen/:nomor", controllers.GetDokumenByNomor)

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

		customer.POST(
			"/dokumen/upload",
			controllers.UploadDokumen,
		)
	}
	// ======================
	// ADMIN & OWNER
	// ======================

	admin := r.Group("/admin")
	admin.Use(
		middleware.AuthMiddleware(),
		middleware.RoleMiddleware("admin", "owner"),
	)

	{
		admin.GET(
			"/dashboard",
			controllers.GetDashboard,
		)

		admin.GET(
			"/pendaftaran",
			controllers.GetAllPendaftaran,
		)

		admin.GET(
			"/pendaftaran/:nomor",
			controllers.GetDetailPendaftaran,
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
			"/pembayaran/pending",
			controllers.GetPendingPembayaran,
		)

		admin.PUT(
			"/dokumen/:id/verifikasi",
			controllers.VerifikasiDokumen,
		)

		admin.GET(
			"/dokumen/:id",
			controllers.GetDetailDokumen,
		)

		admin.GET(
			"/dokumen/pending",
			controllers.GetPendingDokumen,
		)

		admin.POST(
			"/paket",
			controllers.CreatePaket,
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

		admin.DELETE(
			"/paket/:id",
			controllers.DeletePaket,
		)

	}

	// ======================
	// OWNER ONLY
	// ======================

	owner := r.Group("/owner")
	owner.Use(
		middleware.AuthMiddleware(),
		middleware.RoleMiddleware("owner"),
	)

	{
		owner.POST("/admin", controllers.CreateAdmin)
		owner.GET("/admin", controllers.GetAdminList)
		owner.DELETE("/admin/:id", controllers.DeleteAdmin)
	}
}
