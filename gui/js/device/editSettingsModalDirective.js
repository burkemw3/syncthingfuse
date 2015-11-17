angular.module('syncthingfuse.device')
    .directive('editSettingsModal', function () {
        return {
            restrict: 'A',
            templateUrl: 'js/device/editSettingsModalView.html'
        };
});
