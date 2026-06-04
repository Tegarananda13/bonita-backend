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

	r.POST("/pembayaran", controllers.CreatePembayaran)
	r.GET("/pembayaran/:nomor", controllers.GetPembayaranByNomor)
	r.POST("/pembayaran/:id/upload", controllers.UploadBuktiPembayaran)

	r.POST("/dokumen/upload", controllers.UploadDokumen)
	r.GET("/dokumen/:nomor", controllers.GetDokumenByNomor)

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
		
		admin.PUT(
			"/pembayaran/:id/verifikasi",
			controllers.VerifikasiPembayaran,
		)

		admin.PUT(
			"/dokumen/:id/verifikasi",
			controllers.VerifikasiDokumen,
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
		// owner.GET("/admin", controllers.GetAdminList)
	}
}