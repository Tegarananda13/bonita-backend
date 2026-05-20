package routes

import (
	"bonita-backend/controllers"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine) {
	r.POST("/pendaftaran", controllers.CreatePendaftaran)
	r.GET("/pendaftaran/:nomor", controllers.GetPendaftaranByNomor)

	r.POST("/otp/request", controllers.RequestOTP)
	r.POST("/otp/verify", controllers.VerifyOTP)

	r.POST("/pembayaran", controllers.CreatePembayaran)
	r.GET("/pembayaran/:nomor", controllers.GetPembayaranByNomor)
	r.POST("/pembayaran/:id/upload", controllers.UploadBuktiPembayaran)
	r.PUT("/pembayaran/:id/verifikasi", controllers.VerifikasiPembayaran)

	r.POST("/dokumen/upload", controllers.UploadDokumen)
	r.GET("/dokumen/:nomor", controllers.GetDokumenByNomor)
	r.PUT("/dokumen/:id/verifikasi", controllers.VerifikasiDokumen)
}
