package server

import (
	context "context"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

func (s *EndNodeServer) ObtainNewCert(ctx context.Context, obtainNewCertReq *proto.ObtainNewCertReq) (*proto.ObtainNewCertRsp, error) {
	obtainNewCertRsp := &proto.ObtainNewCertRsp{Code: 0}
	err := s.certManager.ObtainNewCert(obtainNewCertReq.GetDomain())
	if err != nil {
		obtainNewCertRsp.Code = 1020
		obtainNewCertRsp.Msg = err.Error()
	}
	return obtainNewCertRsp, nil
}

func (s *EndNodeServer) TransferCert(ctx context.Context, transferCertReq *proto.TransferCertReq) (*proto.TransferCertRsp, error) {
	transferCertRsp := &proto.TransferCertRsp{Code: 0}
	if err := s.certManager.AddCertificates(
		transferCertReq.Domain,
		transferCertReq.KeyDatas,
		transferCertReq.CertData,
	); err != nil {
		transferCertRsp.Code = 1030
		transferCertRsp.Msg = err.Error()
	}
	return transferCertRsp, nil
}

func (s *EndNodeServer) GetCerts(ctx context.Context, getCertsReq *proto.GetCertsReq) (*proto.GetCertsRsp, error) {
	getCertsRsp := &proto.GetCertsRsp{Code: 0}
	getCertsRsp.Certs = s.certManager.GetAllCert()
	return getCertsRsp, nil
}

func (s *EndNodeServer) DeleteCert(ctx context.Context, deleteCertReq *proto.DeleteCertReq) (*proto.DeleteCertRsp, error) {
	deleteCertRsp := &proto.DeleteCertRsp{Code: 0}
	if err := s.certManager.DeleteCert(deleteCertReq.GetDomain()); err != nil {
		deleteCertRsp.Code = 1040
		deleteCertRsp.Msg = err.Error()
	}
	return deleteCertRsp, nil
}
