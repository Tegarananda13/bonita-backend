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
			"/pendaftaran/:id/assign",
			controllers.AssignPendaftaran,
		)

		admin.PUT(
			"/pendaftaran/:id/selesai",
			controllers.TandaiSelesai,
		)

		admin.GET(
			"/pendaftaran/:nomor",
			controllers.GetDetailPendaftaran,
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
